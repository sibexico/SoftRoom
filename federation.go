package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	cryptossh "golang.org/x/crypto/ssh"
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

type Federation struct {
	servers []*ServerConnection
	hub     *Hub
}

func NewFederation(hub *Hub, serverAddresses []string) *Federation {
	f := &Federation{
		hub: hub,
	}
	for _, addr := range serverAddresses {
		sc := NewServerConnection(addr, hub)
		f.servers = append(f.servers, sc)
	}
	return f
}

func (f *Federation) Start() {
	for _, sc := range f.servers {
		go sc.Connect()
	}
}

func (f *Federation) HandleNewServer(s ssh.Session) {
	log.Printf("Handling new federation connection from %s", s.RemoteAddr().String())
	remoteHost, _, err := net.SplitHostPort(s.RemoteAddr().String())
	if err != nil {
		log.Printf("Failed to parse remote address: %v", err)
		return
	}

	for _, sc := range f.servers {
		serverHost, _, err := net.SplitHostPort(sc.addr)
		if err != nil {
			log.Printf("Failed to parse server address: %v", err)
			continue
		}
		if remoteHost == serverHost {
			sc.stdin = s
			go sc.handleConnection(s)
			go sc.startNickSync(s)
			<-s.Context().Done()
			log.Printf("Federation connection from %s closed", s.RemoteAddr().String())
			return
		}
	}
	log.Printf("Ignoring connection from unknown server %s", s.RemoteAddr().String())
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
	addr  string
	hub   *Hub
	stdin io.Writer
}

func NewServerConnection(addr string, hub *Hub) *ServerConnection {
	return &ServerConnection{
		addr: addr,
		hub:  hub,
	}
}

func (sc *ServerConnection) Connect() {
	config := &cryptossh.ClientConfig{
		User:            "federation",
		Auth:            []cryptossh.AuthMethod{},
		HostKeyCallback: cryptossh.InsecureIgnoreHostKey(),
	}

	client, err := cryptossh.Dial("tcp", sc.addr, config)
	if err != nil {
		log.Printf("Failed to dial federated server at %s: %v", sc.addr, err)
		return
	}

	session, err := client.NewSession()
	if err != nil {
		log.Printf("Failed to create session with federated server at %s: %v", sc.addr, err)
		return
	}
	defer session.Close()

	sc.stdin, err = session.StdinPipe()
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

	log.Printf("Successfully connected to federated server at %s", sc.addr)

	go sc.handleConnection(stdout)
	go sc.startNickSync(sc.stdin)

	session.Wait()
}

func (sc *ServerConnection) handleConnection(stdout io.Reader) {
	decoder := json.NewDecoder(stdout)
	for {
		var msg FederationMessage
		if err := decoder.Decode(&msg); err != nil {
			log.Printf("Failed to decode message from federated server: %v", err)
			return
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
		}
	}
}

func (sc *ServerConnection) startNickSync(stdin io.Writer) {
	sc.sendNickSync(stdin)

	ticker := time.NewTicker(5 * time.Second) // Increased frequency for better responsiveness
	defer ticker.Stop()

	for range ticker.C {
		sc.sendNickSync(stdin)
	}
}

func (sc *ServerConnection) sendNickSync(stdin io.Writer) {
	nicks := sc.hub.getLocalUserList()
	payload := NickSyncPayload{Nicks: nicks}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal nick_sync payload: %v", err)
		return
	}

	msg := FederationMessage{Type: "nick_sync", Payload: b}
	b, err = json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal federation message: %v", err)
		return
	}

	fmt.Fprintln(stdin, string(b))
}

func (sc *ServerConnection) sendPrivateMessage(from, to, text string) {
	payload := PrivateMessagePayload{From: from, To: to, Text: text}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal private_message payload: %v", err)
		return
	}

	msg := FederationMessage{Type: "private_message", Payload: b}
	b, err = json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal federation message: %v", err)
		return
	}

	fmt.Fprintln(sc.stdin, string(b))
}

func (sc *ServerConnection) sendNameChange(oldName, newName string, isGitHubAuth bool) {
	payload := NameChangePayload{OldName: oldName, NewName: newName, IsGitHubAuth: isGitHubAuth}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal name_change payload: %v", err)
		return
	}

	msg := FederationMessage{Type: "name_change", Payload: b}
	b, err = json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal federation message: %v", err)
		return
	}

	fmt.Fprintln(sc.stdin, string(b))
}
