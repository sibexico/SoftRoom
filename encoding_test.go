package main

import "testing"

func TestDefaultSessionCharset(t *testing.T) {
	if defaultSessionCharset != "utf-8" {
		t.Fatalf("defaultSessionCharset = %q, want %q", defaultSessionCharset, "utf-8")
	}
}
