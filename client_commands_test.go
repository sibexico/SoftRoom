package main

import (
	"testing"
	"time"
)

func TestClientStartAuthAttemptCooldown(t *testing.T) {
	c := &Client{send: make(chan Message, 1)}

	ok, wait := c.StartAuthAttempt(50 * time.Millisecond)
	if !ok || wait != 0 {
		t.Fatalf("first StartAuthAttempt = (%v, %s), want (true, 0)", ok, wait)
	}

	ok, wait = c.StartAuthAttempt(50 * time.Millisecond)
	if ok || wait != 0 {
		t.Fatalf("second StartAuthAttempt while in progress = (%v, %s), want (false, 0)", ok, wait)
	}

	c.FinishAuthAttempt()
	ok, wait = c.StartAuthAttempt(2 * time.Second)
	if ok || wait <= 0 {
		t.Fatalf("StartAuthAttempt during cooldown = (%v, %s), want (false, >0)", ok, wait)
	}
}

func TestHandleCommandBasicCases(t *testing.T) {
	h := &Hub{
		changeName:   make(chan nameChangeRequest, 1),
		requestUsers: make(chan chan []string, 1),
	}
	c := &Client{hub: h, user: "anon", send: make(chan Message, 10)}
	cfg := &Config{}

	if msg, handled := handleCommand(c, "hello", cfg); handled || msg != (Message{}) {
		t.Fatal("non-command input should not be handled")
	}

	if msg, handled := handleCommand(c, "/h", cfg); !handled || msg.Type != "system" {
		t.Fatal("/h should return a system help message")
	}

	if msg, handled := handleCommand(c, "/n aa", cfg); !handled || msg.Type != "system" {
		t.Fatal("invalid /n should return a system usage/validation message")
	}

	if msg, handled := handleCommand(c, "/n New_Name", cfg); !handled || msg != (Message{}) {
		t.Fatal("valid /n should be handled asynchronously by hub")
	}

	select {
	case req := <-h.changeName:
		if req.newName != "New_Name" {
			t.Fatalf("name change requested with %q", req.newName)
		}
	default:
		t.Fatal("expected a name change request on hub.changeName")
	}

	if msg, handled := handleCommand(c, "/w target", cfg); !handled || msg.Type != "system" {
		t.Fatal("/w with missing text should return usage system message")
	}
}
