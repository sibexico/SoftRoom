package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gliderlabs/ssh"
)

func main() {
	cfg, err := LoadConfig("softroom.ini")
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("Config file not found. Creating a default 'softroom.ini'.")
			log.Println("Please edit it with your GitHub OAuth Client ID.")
			if err := CreateDefaultConfig("softroom.ini"); err != nil {
				log.Fatalf("Failed to create default config: %v", err)
			}
			os.Exit(1)
		}
		log.Fatalf("Failed to load configuration: %v", err)
	}

	hub := newHub()
	go hub.run()

	sshHandler := func(s ssh.Session) {
		pty, _, active := s.Pty()
		if !active {
			fmt.Fprintln(s, "A PTY is required to run SoftRoom.")
			s.Close()
			return
		}

		// Assign an anonymous name initially
		initialName := generateAnonymousName()
		client := NewClient(s, hub, initialName)

		// Send a welcome message with instructions for changing name or authenticating
		welcomeText := fmt.Sprintf("Welcome, %s! Use /n <newname> to change your name, or /gh to authenticate with GitHub.", initialName)
		client.send <- systemMessage(welcomeText)

		hub.register <- client

		client.RunTUI(pty.Window.Width, pty.Window.Height, cfg.Chat.WelcomeMessage, cfg)

		hub.unregister <- client
	}
	server := ssh.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: sshHandler,
		PtyCallback: func(ctx ssh.Context, pty ssh.Pty) bool {
			// Always accept PTY requests
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
