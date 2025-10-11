package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gliderlabs/ssh"
	cryptossh "golang.org/x/crypto/ssh"
	"gopkg.in/ini.v1"
)

type Config struct {
	Server struct {
		Host        string `ini:"host"`
		Port        int    `ini:"port"`
		HostKeyPath string `ini:"host_key_path"`
	} `ini:"server"`
	GitHubAuth struct {
		ClientID string `ini:"client_id"`
	} `ini:"github_auth"`
	Chat struct {
		WelcomeMessage string `ini:"welcome_message"`
	} `ini:"chat"`
	Federation struct {
		Servers []string `ini:"servers,omitempty,allowshadow"`
	} `ini:"federation"`
}

func LoadConfig(path string) (*Config, error) {
	cfg := new(Config)
	// Default values
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 2222
	cfg.Server.HostKeyPath = "./id_rsa"
	cfg.Chat.WelcomeMessage = "Welcome to SoftRoom!"

	// MapTo will load the file and override defaults
	err := ini.MapTo(cfg, path)
	if err != nil {
		return nil, err
	}

	if cfg.GitHubAuth.ClientID == "" {
		return nil, fmt.Errorf("`client_id` in section `github_auth` must be set in %s", path)
	}

	return cfg, nil
}

// CreateDefaultConfig creates a default softroom.ini file for the user
func CreateDefaultConfig(path string) error {
	content := `
; softroom.ini - Configuration for the SoftRoom Server
[server]
; The IP address and port for the SSH server.
; Use 0.0.0.0 to listen on all available network interfaces.
host = 0.0.0.0
port = 2299

; Path to the SSH private host key.
; If the file does not exist, a new one will be generated there at first run.
host_key_path = ./id_rsa

[github_auth]
; The Client ID of your GitHub OAuth App. REQUIRED.
; Create one here: https://github.com/settings/applications/new
client_id = YOUR_GITHUB_OAUTH_CLIENT_ID

[chat]
; The message displayed to users after they successfully log in.
welcome_message = Welcome to SoftRoom based group chat!

[federation]
; A list of other SoftRoom servers to connect to.
; servers = host:port, anotherhost:port
`
	return os.WriteFile(path, []byte(strings.TrimSpace(content)), 0644)
}

func getHostKey(path string) ssh.Signer {
	keyData, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Host key not found at %s. Generating a new one.", path)
		// Generate a new ed25519 key pair
		_, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatalf("Failed to generate host key: %v", err)
		}

		// Encode the private key to PEM
		pemBlock := &pem.Block{
			Type:  "OPENSSH PRIVATE KEY",
			Bytes: marshalED25519PrivateKey(privKey),
		}
		pemBytes := pem.EncodeToMemory(pemBlock)

		// Write the new key to the file
		if err := os.WriteFile(path, pemBytes, 0600); err != nil {
			log.Fatalf("Failed to write new host key: %v", err)
		}
		log.Printf("New host key saved to %s", path)
		keyData = pemBytes
	}

	signer, err := cryptossh.ParsePrivateKey(keyData)
	if err != nil {
		log.Fatalf("Failed to parse host key: %v", err)
	}
	return signer
}

// marshalED25519PrivateKey is a helper to encode ed25519 private key into the OpenSSH format
func marshalED25519PrivateKey(key ed25519.PrivateKey) []byte {
	magic := append([]byte("openssh-key-v1"), 0)
	var w struct {
		CipherName   string
		KdfName      string
		KdfOpts      string
		NumKeys      uint32
		PubKey       []byte
		PrivKeyBlock []byte
	}
	w.CipherName = "none"
	w.KdfName = "none"
	w.KdfOpts = ""
	w.NumKeys = 1

	pk1 := struct {
		Check1  uint32
		Check2  uint32
		Keytype string
		Pub     []byte
		Priv    []byte
		Comment string
		Pad     []byte `ssh:"rest"`
	}{}

	pk1.Check1 = 0xdeadbeef
	pk1.Check2 = 0xdeadbeef

	pk1.Keytype = cryptossh.KeyAlgoED25519
	pk1.Pub = key.Public().(ed25519.PublicKey)
	pk1.Priv = key
	pk1.Comment = ""
	var privKeyBlockBytes []byte
	privKeyBlockBytes = append(privKeyBlockBytes, cryptossh.Marshal(pk1)...)
	padLen := 8 - (len(privKeyBlockBytes) % 8)
	if padLen < 4 {
		padLen += 8
	}
	pk1.Pad = make([]byte, padLen)
	for i := 0; i < padLen; i++ {
		pk1.Pad[i] = byte(i + 1)
	}
	w.PrivKeyBlock = cryptossh.Marshal(pk1)

	pubKeyBytes, err := cryptossh.NewPublicKey(key.Public())
	if err != nil {
		panic(err)
	}
	w.PubKey = pubKeyBytes.Marshal()
	message := cryptossh.Marshal(w)

	return append(magic, message...)
}
