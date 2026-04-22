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

	"github.com/charmbracelet/ssh"
)

func main() {
	configPath := flag.String("c", "softroom.ini", "path to config file")
	flag.Parse()
	workingDir := mustGetwd()

	safeConfigPath, err := sanitizePathInBase(*configPath, workingDir, "config file path")
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
	federation, err := NewFederation(hub, cfg.Federation.Servers, safeKnownHostsPath, cfg.Federation.SharedSecret)
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
			_ = s.Close()
			return
		}

		sio := buildSessionIO(s)

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
			getHostKey(safeHostKeyPath),
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

	if resolvedBase, err := filepath.EvalSymlinks(absBase); err == nil {
		absBase = resolvedBase
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve base symlinks: %w", err)
	}

	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(absBase, cleaned)
	}

	absPath, err := filepath.Abs(filepath.Clean(cleaned))
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", fieldName, err)
	}

	relativeCheckPath := absPath
	if info, err := os.Lstat(absPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s cannot be a symlink", fieldName)
		}
		if resolvedPath, err := filepath.EvalSymlinks(absPath); err == nil {
			relativeCheckPath = resolvedPath
		} else {
			return "", fmt.Errorf("resolve %s symlinks: %w", fieldName, err)
		}
	} else if os.IsNotExist(err) {
		parent := filepath.Dir(absPath)
		resolvedParent, err := filepath.EvalSymlinks(parent)
		if err == nil {
			relativeCheckPath = filepath.Join(resolvedParent, filepath.Base(absPath))
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve parent for %s: %w", fieldName, err)
		}
	} else {
		return "", fmt.Errorf("lstat %s: %w", fieldName, err)
	}

	relPath, err := filepath.Rel(absBase, relativeCheckPath)
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
