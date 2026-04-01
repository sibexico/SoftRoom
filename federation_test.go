package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureKnownHostsFileAndNewFederationValidation(t *testing.T) {
	dir := t.TempDir()
	knownHosts := filepath.Join(dir, "known_hosts")

	if err := ensureKnownHostsFile(knownHosts); err != nil {
		t.Fatalf("ensureKnownHostsFile(create) error: %v", err)
	}

	if err := ensureKnownHostsFile(knownHosts); err != nil {
		t.Fatalf("ensureKnownHostsFile(existing) error: %v", err)
	}

	h := newHub()
	if _, err := NewFederation(h, []string{"server:22"}, knownHosts, ""); err == nil {
		t.Fatal("NewFederation should fail with servers configured and empty shared secret")
	}

	f, err := NewFederation(h, []string{"server:22"}, knownHosts, "secret")
	if err != nil {
		t.Fatalf("NewFederation(valid) error: %v", err)
	}
	if len(f.servers) != 1 {
		t.Fatalf("len(f.servers) = %d, want 1", len(f.servers))
	}
}

func TestServerConnectionSendAuthAndRawMessage(t *testing.T) {
	sc := NewServerConnection("server:22", newHub(), "", "secret")

	if err := sc.sendAuth(); err == nil {
		t.Fatal("sendAuth should fail when connection writer is nil")
	}

	buf := &bytes.Buffer{}
	sc.setConnection(buf)
	if err := sc.sendAuth(); err != nil {
		t.Fatalf("sendAuth with writer error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"type":"auth"`) {
		t.Fatalf("expected auth message in output, got %q", output)
	}
}

func TestHandleConnectionAuthAndDispatch(t *testing.T) {
	h := &Hub{
		syncNicks:        make(chan nickSyncRequest, 1),
		privateMsgChan:   make(chan privateMessagePayload, 1),
		remoteNameChange: make(chan remoteNameChangeRequest, 1),
	}

	sc := NewServerConnection("server:22", h, "", "shared")

	buildLine := func(msgType string, payload any) string {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		msg := FederationMessage{Type: msgType, Payload: b}
		line, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal message: %v", err)
		}
		return string(line)
	}

	input := strings.Join([]string{
		buildLine("auth", AuthPayload{Secret: "shared"}),
		buildLine("nick_sync", NickSyncPayload{Nicks: []string{"Alice", "Bob"}}),
		buildLine("private_message", PrivateMessagePayload{From: "Alice", To: "Bob", Text: "hi"}),
		buildLine("name_change", NameChangePayload{OldName: "Alice", NewName: "Alice2", IsGitHubAuth: true}),
	}, "\n") + "\n"

	sc.handleConnection(strings.NewReader(input))

	if !sc.isAuthenticated() {
		t.Fatal("server connection should be authenticated after valid auth message")
	}

	select {
	case req := <-h.syncNicks:
		if req.serverAddr != "server:22" || len(req.nicks) != 2 {
			t.Fatalf("unexpected sync request: %+v", req)
		}
	default:
		t.Fatal("expected nick sync request")
	}

	select {
	case p := <-h.privateMsgChan:
		if p.TargetUser != "Bob" || p.Message.Content != "hi" {
			t.Fatalf("unexpected private message payload: %+v", p)
		}
	default:
		t.Fatal("expected private message payload")
	}

	select {
	case req := <-h.remoteNameChange:
		if req.newName != "Alice2" || !req.isGitHubAuth {
			t.Fatalf("unexpected remote name change request: %+v", req)
		}
	default:
		t.Fatal("expected remote name change request")
	}
}
