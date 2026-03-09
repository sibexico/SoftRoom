package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"
)

func main() {
	configPath := flag.String("c", "softroom.ini", "path to config file")
	flag.Parse()

	safeConfigPath, err := sanitizePathInBase(*configPath, mustGetwd(), "config file path")
	if err != nil {
		log.Fatalf("Invalid config path: %v", err)
	}

	cfg, err := LoadConfig(safeConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Config file not found at %s. Creating a default one.", safeConfigPath)
			log.Println("Please edit it with your GitHub OAuth Client ID.")
			if err := CreateDefaultConfig(safeConfigPath); err != nil {
				log.Fatalf("Failed to create default config: %v", err)
			}
			os.Exit(1)
		}
		log.Fatalf("Failed to load configuration: %v", err)
	}

	hostKeyBase := filepath.Dir(safeConfigPath)
	safeHostKeyPath, err := sanitizePathInBase(cfg.Server.HostKeyPath, hostKeyBase, "host key path")
	if err != nil {
		log.Fatalf("Invalid host key path in config: %v", err)
	}
	cfg.Server.HostKeyPath = safeHostKeyPath

	safeKnownHostsPath, err := sanitizePathInBase(cfg.Federation.KnownHostsPath, hostKeyBase, "federation known_hosts path")
	if err != nil {
		log.Fatalf("Invalid federation known_hosts path in config: %v", err)
	}
	cfg.Federation.KnownHostsPath = safeKnownHostsPath

	hub := newHub()
	federation, err := NewFederation(hub, cfg.Federation.Servers, cfg.Federation.KnownHostsPath, cfg.Federation.SharedSecret)
	if err != nil {
		log.Fatalf("Failed to initialize federation: %v", err)
	}
	hub.federation = federation
	go hub.run()

	federation.Start()

	sshHandler := func(s ssh.Session) {
		if s.User() == "federation" {
			hub.federation.HandleNewServer(s)
			return
		}

		pty, _, active := s.Pty()
		if !active {
			fmt.Fprintln(s, "A PTY is required to run SoftRoom.")
			s.Close()
			return
		}

		sio := buildSessionIO(s)
		if !sio.charsetKnown {
			fmt.Fprintln(s, "Warning: Unknown terminal charset. Set LANG/LC_CTYPE to UTF-8 for correct non-Latin messages.")
		}

		initialName := generateAnonymousName()
		client := NewClient(s, hub, "", sio.input, sio.output)
		client.SetUser(initialName)

		welcomeText := fmt.Sprintf("Welcome, %s! Use /n <newname> to change your name, or /gh to authenticate with GitHub.", initialName)
		client.EnqueueMessage(SystemMessage(welcomeText))

		hub.register <- client

		client.RunTUI(pty.Window.Width, pty.Window.Height, cfg.Chat.WelcomeMessage, cfg)

		hub.unregister <- client
	}
	server := ssh.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: sshHandler,
		PtyCallback: func(ctx ssh.Context, pty ssh.Pty) bool {
			return true
		},
		HostSigners: []ssh.Signer{
			getHostKey(cfg.Server.HostKeyPath),
		},
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("Starting SoftRoom SSH server at %s:%d", cfg.Server.Host, cfg.Server.Port)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != ssh.ErrServerClosed {
			log.Fatalf("Could not start SSH server: %v", err)
		}
	}()

	<-done
	log.Println("Server is shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %+v", err)
	}
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get working directory: %v", err)
	}
	return wd
}

func sanitizePathInBase(inputPath, baseDir, fieldName string) (string, error) {
	cleaned := strings.TrimSpace(inputPath)
	if cleaned == "" {
		return "", fmt.Errorf("%s cannot be empty", fieldName)
	}

	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve base directory: %w", err)
	}

	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(absBase, cleaned)
	}

	absPath, err := filepath.Abs(filepath.Clean(cleaned))
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", fieldName, err)
	}

	relPath, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return "", fmt.Errorf("validate %s: %w", fieldName, err)
	}

	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%s must stay inside %s", fieldName, absBase)
	}

	if info, err := os.Stat(absPath); err == nil && info.IsDir() {
		return "", fmt.Errorf("%s points to a directory", fieldName)
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", fieldName, err)
	}

	return absPath, nil
}
