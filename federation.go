package main

import (
	"bufio"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	cryptossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type FederationMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type NickSyncPayload struct {
	Nicks []string `json:"nicks"`
}

type PrivateMessagePayload struct {
	From string `json:"from"`
	To   string `json:"to"`
	Text string `json:"text"`
}

type NameChangePayload struct {
	OldName      string `json:"old_name"`
	NewName      string `json:"new_name"`
	IsGitHubAuth bool   `json:"is_github_auth"`
}

type AuthPayload struct {
	Secret string `json:"secret"`
}

type Federation struct {
	servers []*ServerConnection
	hub     *Hub
}

const federationAuthTimeout = 15 * time.Second

func NewFederation(hub *Hub, serverAddresses []string, knownHostsPath, sharedSecret string) (*Federation, error) {
	if len(serverAddresses) > 0 {
		if strings.TrimSpace(sharedSecret) == "" {
			return nil, errors.New("shared federation secret is required when federation servers are configured")
		}
		if err := ensureKnownHostsFile(knownHostsPath); err != nil {
			return nil, fmt.Errorf("prepare known_hosts file: %w", err)
		}
	}

	f := &Federation{
		hub: hub,
	}
	for _, addr := range serverAddresses {
		sc := NewServerConnection(addr, hub, knownHostsPath, sharedSecret)
		f.servers = append(f.servers, sc)
	}
	return f, nil
}

func (f *Federation) Start() {
	for _, sc := range f.servers {
		go sc.Connect()
	}
}

func ensureKnownHostsFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("known_hosts path cannot be empty")
	}

	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("known_hosts path %s is a directory", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return f.Close()
}

func (sc *ServerConnection) setConnection(stdin io.Writer) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.stdin = stdin
	sc.authenticated = false
}

func (sc *ServerConnection) resetConnection() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.stdin = nil
	sc.authenticated = false
}

func (sc *ServerConnection) getConnectionWriter() io.Writer {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.stdin
}

func (sc *ServerConnection) setAuthenticated(authenticated bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.authenticated = authenticated
}

func (sc *ServerConnection) isAuthenticated() bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.authenticated
}

func (f *Federation) HandleNewServer(s ssh.Session) {
	log.Printf("Handling new federation connection from %s", s.RemoteAddr().String())
	remoteHost, _, err := net.SplitHostPort(s.RemoteAddr().String())
	if err != nil {
		log.Printf("Failed to parse remote address: %v", err)
		_ = s.Close()
		return
	}

	for _, sc := range f.servers {
		serverHost, _, err := net.SplitHostPort(sc.addr)
		if err != nil {
			log.Printf("Failed to parse server address: %v", err)
			continue
		}
		if remoteHost == serverHost {
			sc.setConnection(s)
			if err := sc.sendAuth(); err != nil {
				log.Printf("Failed to send federation auth to %s: %v", sc.addr, err)
				sc.resetConnection()
				_ = s.Close()
				return
			}

			go func(sess ssh.Session, conn *ServerConnection) {
				select {
				case <-time.After(federationAuthTimeout):
					if !conn.isAuthenticated() {
						log.Printf("Closing unauthenticated federation session from %s after timeout", sess.RemoteAddr().String())
						_ = sess.Close()
					}
				case <-sess.Context().Done():
				}
			}(s, sc)

			go sc.handleConnection(s)
			go sc.startNickSync()
			<-s.Context().Done()
			sc.resetConnection()
			log.Printf("Federation connection from %s closed", s.RemoteAddr().String())
			return
		}
	}
	log.Printf("Ignoring connection from unknown server %s", s.RemoteAddr().String())
	_ = s.Close()
}

func (f *Federation) BroadcastNameChange(oldName, newName string, isGitHubAuth bool) {
	// Create a WaitGroup to ensure all servers receive the update
	var wg sync.WaitGroup
	for _, s := range f.servers {
		wg.Add(1)
		go func(server *ServerConnection) {
			defer wg.Done()
			server.sendNameChange(oldName, newName, isGitHubAuth)
		}(s)
	}
	wg.Wait()
}

type ServerConnection struct {
	addr           string
	hub            *Hub
	knownHostsPath string
	sharedSecret   string

	mu            sync.RWMutex
	writeMu       sync.Mutex
	stdin         io.Writer
	authenticated bool
}

func NewServerConnection(addr string, hub *Hub, knownHostsPath, sharedSecret string) *ServerConnection {
	return &ServerConnection{
		addr:           addr,
		hub:            hub,
		knownHostsPath: knownHostsPath,
		sharedSecret:   sharedSecret,
	}
}

func (sc *ServerConnection) Connect() {
	hostKeyCallback, err := knownhosts.New(sc.knownHostsPath)
	if err != nil {
		log.Printf("Failed to load federation known_hosts from %s: %v", sc.knownHostsPath, err)
		return
	}

	config := &cryptossh.ClientConfig{
		User:            "federation",
		Auth:            []cryptossh.AuthMethod{},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	client, err := cryptossh.Dial("tcp", sc.addr, config)
	if err != nil {
		log.Printf("Failed to dial federated server at %s: %v", sc.addr, err)
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		log.Printf("Failed to create session with federated server at %s: %v", sc.addr, err)
		return
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		log.Printf("Failed to get stdin pipe: %v", err)
		return
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		log.Printf("Failed to get stdout pipe: %v", err)
		return
	}

	modes := cryptossh.TerminalModes{
		cryptossh.ECHO:          0,
		cryptossh.TTY_OP_ISPEED: 14400,
		cryptossh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
		log.Printf("request for pseudo terminal failed: %s", err)
		return
	}

	if err := session.Shell(); err != nil {
		log.Printf("failed to start shell: %s", err)
		return
	}

	sc.setConnection(stdin)
	if err := sc.sendAuth(); err != nil {
		log.Printf("Failed to send federation auth to %s: %v", sc.addr, err)
		sc.resetConnection()
		return
	}

	log.Printf("Successfully connected to federated server at %s", sc.addr)

	go func() {
		time.Sleep(federationAuthTimeout)
		if !sc.isAuthenticated() {
			log.Printf("Closing unauthenticated outbound federation session to %s after timeout", sc.addr)
			_ = session.Close()
		}
	}()

	go sc.handleConnection(stdout)
	go sc.startNickSync()

	if err := session.Wait(); err != nil {
		log.Printf("Federation session with %s closed: %v", sc.addr, err)
	}
	sc.resetConnection()
}

func (sc *ServerConnection) handleConnection(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		var msg FederationMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("Failed to decode federation message from %s: %v", sc.addr, err)
			continue
		}

		if msg.Type == "auth" {
			var payload AuthPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Printf("Failed to decode federation auth payload from %s: %v", sc.addr, err)
				continue
			}

			if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(payload.Secret)), []byte(strings.TrimSpace(sc.sharedSecret))) != 1 {
				log.Printf("Federation auth failed for %s", sc.addr)
				return
			}

			if !sc.isAuthenticated() {
				log.Printf("Federation link authenticated for %s", sc.addr)
			}
			sc.setAuthenticated(true)
			continue
		}

		if !sc.isAuthenticated() {
			log.Printf("Ignoring unauthenticated federation message type %q from %s", msg.Type, sc.addr)
			continue
		}

		switch msg.Type {
		case "nick_sync":
			var payload NickSyncPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Printf("Failed to unmarshal nick_sync payload: %v", err)
				continue
			}
			sc.hub.syncNicks <- nickSyncRequest{serverAddr: sc.addr, nicks: payload.Nicks}
		case "private_message":
			var payload PrivateMessagePayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Printf("Failed to unmarshal private_message payload: %v", err)
				continue
			}
			sc.hub.sendPrivateMessage(payload.To, Message{Author: payload.From, Content: payload.Text, Type: "private"}, nil)
		case "name_change":
			var payload NameChangePayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Printf("Failed to unmarshal name_change payload: %v", err)
				continue
			}
			sc.hub.remoteNameChange <- remoteNameChangeRequest{oldName: payload.OldName, newName: payload.NewName, isGitHubAuth: payload.IsGitHubAuth, serverAddr: sc.addr}
		default:
			log.Printf("Ignoring unknown federation message type %q from %s", msg.Type, sc.addr)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Federation reader error for %s: %v", sc.addr, err)
	}
}

func (sc *ServerConnection) startNickSync() {
	sc.sendNickSync()

	ticker := time.NewTicker(5 * time.Second) // Increased frequency for better responsiveness
	defer ticker.Stop()

	for range ticker.C {
		if sc.getConnectionWriter() == nil {
			return
		}
		sc.sendNickSync()
	}
}

func (sc *ServerConnection) sendNickSync() {
	if !sc.isAuthenticated() {
		return
	}

	stdin := sc.getConnectionWriter()
	if stdin == nil {
		log.Printf("Skipping nick sync for %s: connection is not ready", sc.addr)
		return
	}

	nicks := sc.hub.getLocalUserList()
	payload := NickSyncPayload{Nicks: nicks}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal nick_sync payload: %v", err)
		return
	}

	msg := FederationMessage{Type: "nick_sync", Payload: b}
	if err := sc.sendRawMessage(stdin, msg); err != nil {
		log.Printf("Failed to send nick sync to %s: %v", sc.addr, err)
		return
	}
}

func (sc *ServerConnection) sendPrivateMessage(from, to, text string) {
	if !sc.isAuthenticated() {
		log.Printf("Skipping private message via %s: federation link not authenticated", sc.addr)
		return
	}

	stdin := sc.getConnectionWriter()
	if stdin == nil {
		log.Printf("Skipping private message to %s via %s: connection is not ready", to, sc.addr)
		return
	}

	payload := PrivateMessagePayload{From: from, To: to, Text: text}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal private_message payload: %v", err)
		return
	}

	msg := FederationMessage{Type: "private_message", Payload: b}
	if err := sc.sendRawMessage(stdin, msg); err != nil {
		log.Printf("Failed to send private message via %s: %v", sc.addr, err)
		return
	}
}

func (sc *ServerConnection) sendNameChange(oldName, newName string, isGitHubAuth bool) {
	if !sc.isAuthenticated() {
		log.Printf("Skipping name change via %s: federation link not authenticated", sc.addr)
		return
	}

	stdin := sc.getConnectionWriter()
	if stdin == nil {
		log.Printf("Skipping name change broadcast to %s: connection is not ready", sc.addr)
		return
	}

	payload := NameChangePayload{OldName: oldName, NewName: newName, IsGitHubAuth: isGitHubAuth}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal name_change payload: %v", err)
		return
	}

	msg := FederationMessage{Type: "name_change", Payload: b}
	if err := sc.sendRawMessage(stdin, msg); err != nil {
		log.Printf("Failed to send name change via %s: %v", sc.addr, err)
		return
	}

}

func (sc *ServerConnection) sendAuth() error {
	payload := AuthPayload{Secret: sc.sharedSecret}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	msg := FederationMessage{Type: "auth", Payload: b}
	stdin := sc.getConnectionWriter()
	if stdin == nil {
		return errors.New("connection writer is not ready")
	}

	if err := sc.sendRawMessage(stdin, msg); err != nil {
		return err
	}

	return nil
}

func (sc *ServerConnection) sendRawMessage(stdin io.Writer, msg FederationMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	sc.writeMu.Lock()
	defer sc.writeMu.Unlock()

	if _, err := fmt.Fprintln(stdin, string(b)); err != nil {
		return err
	}

	return nil
}
