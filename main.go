package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"
)

func main() {
	configPath := flag.String("c", "softroom.ini", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Config file not found at %s. Creating a default one.", *configPath)
			log.Println("Please edit it with your GitHub OAuth Client ID.")
			if err := CreateDefaultConfig(*configPath); err != nil {
				log.Fatalf("Failed to create default config: %v", err)
			}
			os.Exit(1)
		}
		log.Fatalf("Failed to load configuration: %v", err)
	}

	hub := newHub()
	federation := NewFederation(hub, cfg.Federation.Servers)
	hub.federation = federation
	go hub.run()

	federation.Start()

	sshHandler := func(s ssh.Session) {
		if s.User() == "federation" {
			hub.federation.HandleNewServer(s)
			return
		}

		lang := ""
		for _, env := range s.Environ() {
			if strings.HasPrefix(env, "LANG=") {
				lang = strings.TrimPrefix(env, "LANG=")
				break
			}
		}
		if !strings.Contains(strings.ToLower(lang), "utf-8") && !strings.Contains(strings.ToLower(lang), "utf8") {
			fmt.Fprintln(s, "Warning: Your client does not appear to support UTF-8. Non-ASCII characters may not be displayed correctly.")
		}

		pty, _, active := s.Pty()
		if !active {
			fmt.Fprintln(s, "A PTY is required to run SoftRoom.")
			s.Close()
			return
		}

		initialName := generateAnonymousName()
		client := NewClient(s, hub, "")
		client.SetUser(initialName)

		welcomeText := fmt.Sprintf("Welcome, %s! Use /n <newname> to change your name, or /gh to authenticate with GitHub.", initialName)
		client.send <- SystemMessage(welcomeText)

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
