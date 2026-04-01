package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cryptossh "golang.org/x/crypto/ssh"
)

func TestLoadConfigSuccessAndValidation(t *testing.T) {
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.ini")
	good := "[github_auth]\nclient_id = abc123\n"
	if err := os.WriteFile(goodPath, []byte(good), 0600); err != nil {
		t.Fatalf("WriteFile good.ini: %v", err)
	}

	cfg, err := LoadConfig(goodPath)
	if err != nil {
		t.Fatalf("LoadConfig(good) error: %v", err)
	}
	if cfg.Server.Port != 2222 {
		t.Fatalf("default server port = %d, want 2222", cfg.Server.Port)
	}

	badPath := filepath.Join(dir, "bad.ini")
	bad := "[server]\nport = 2222\n"
	if err := os.WriteFile(badPath, []byte(bad), 0600); err != nil {
		t.Fatalf("WriteFile bad.ini: %v", err)
	}

	if _, err := LoadConfig(badPath); err == nil {
		t.Fatal("LoadConfig should fail when github_auth.client_id is missing")
	}

	fedPath := filepath.Join(dir, "fed.ini")
	fed := "[github_auth]\nclient_id = abc123\n[federation]\nservers = one:22\n"
	if err := os.WriteFile(fedPath, []byte(fed), 0600); err != nil {
		t.Fatalf("WriteFile fed.ini: %v", err)
	}

	if _, err := LoadConfig(fedPath); err == nil {
		t.Fatal("LoadConfig should fail when federation servers are set without shared_secret")
	}
}

func TestCreateDefaultConfigAndRootFileHelpers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "softroom.ini")

	if err := CreateDefaultConfig(path); err != nil {
		t.Fatalf("CreateDefaultConfig error: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile created config: %v", err)
	}
	if !strings.Contains(string(b), "[github_auth]") {
		t.Fatal("default config does not contain [github_auth] section")
	}

	target := filepath.Join(dir, "x.txt")
	if err := writeFileWithRoot(target, []byte("hello"), 0600); err != nil {
		t.Fatalf("writeFileWithRoot error: %v", err)
	}

	got, err := readFileWithRoot(target)
	if err != nil {
		t.Fatalf("readFileWithRoot error: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("readFileWithRoot() = %q, want hello", string(got))
	}
}

func TestMarshalED25519PrivateKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}

	block := &pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: marshalED25519PrivateKey(priv),
	}

	if _, err := cryptossh.ParsePrivateKey(pem.EncodeToMemory(block)); err != nil {
		t.Fatalf("ParsePrivateKey(marshalED25519PrivateKey) error: %v", err)
	}
}
