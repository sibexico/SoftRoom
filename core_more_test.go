package main

import (
	"path/filepath"
	"testing"
)

func TestSanitizePathInBase(t *testing.T) {
	base := t.TempDir()

	resolved, err := sanitizePathInBase("config.ini", base, "config")
	if err != nil {
		t.Fatalf("sanitizePathInBase relative path error: %v", err)
	}
	if filepath.Dir(resolved) != base {
		t.Fatalf("resolved path %q is outside base %q", resolved, base)
	}

	if _, err := sanitizePathInBase("", base, "config"); err == nil {
		t.Fatal("sanitizePathInBase should fail for empty input")
	}

	if _, err := sanitizePathInBase(filepath.Join("..", "escape.ini"), base, "config"); err == nil {
		t.Fatal("sanitizePathInBase should reject path traversal outside base")
	}
}

func TestClientAccessorsAndEnqueue(t *testing.T) {
	c := NewClient(nil, nil, "anon", nil, nil)
	if c.User() != "anon" {
		t.Fatalf("initial user = %q, want anon", c.User())
	}

	c.SetUser("alice")
	if c.User() != "alice" {
		t.Fatalf("SetUser failed, got %q", c.User())
	}

	if c.IsAuthed() {
		t.Fatal("new client should not be authenticated")
	}

	c.SetIsAuthed(true)
	if !c.IsAuthed() {
		t.Fatal("SetIsAuthed(true) did not persist")
	}

	c.EnqueueMessage(SystemMessage("hello"))
	if len(c.send) != 1 {
		t.Fatalf("expected one queued message, got %d", len(c.send))
	}

	full := &Client{user: "full", send: make(chan Message, 1)}
	full.send <- SystemMessage("one")
	full.EnqueueMessage(SystemMessage("two"))
	if len(full.send) != 1 {
		t.Fatalf("queue size changed unexpectedly for full queue: %d", len(full.send))
	}
}

func TestHubHelperMethodsAndSendToClient(t *testing.T) {
	h := &Hub{
		clients:       make(map[*Client]bool),
		clientsByName: make(map[string]*Client),
		remoteNicks:   map[string][]string{"srv:22": {"remote_user"}},
	}

	c := &Client{user: "alice", send: make(chan Message, 1)}
	h.clients[c] = true
	h.clientsByName[c.User()] = c

	if got, ok := h.findServerForNick("remote_user"); !ok || got != "srv:22" {
		t.Fatalf("findServerForNick returned (%q, %v)", got, ok)
	}

	if !h.isNameTakenInFederation("alice") {
		t.Fatal("local user should be considered taken")
	}

	if !h.isNameTakenInFederation("remote_user") {
		t.Fatal("remote user should be considered taken")
	}

	if h.sendToClient(nil, SystemMessage("x")) {
		t.Fatal("sendToClient(nil, ...) should return false")
	}

	if ok := h.sendToClient(c, SystemMessage("ok")); !ok {
		t.Fatal("sendToClient should succeed when channel has capacity")
	}
	<-c.send

	c.send <- SystemMessage("fill")
	if ok := h.sendToClient(c, SystemMessage("overflow")); ok {
		t.Fatal("sendToClient should return false when channel is full")
	}
	if _, exists := h.clientsByName["alice"]; exists {
		t.Fatal("client should be removed from clientsByName after overflow handling")
	}
}
