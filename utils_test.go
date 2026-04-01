package main

import (
	"regexp"
	"testing"
)

func TestGenerateAnonymousNameFormat(t *testing.T) {
	name := generateAnonymousName()
	re := regexp.MustCompile(`^Anonymous\d{4}$`)
	if !re.MatchString(name) {
		t.Fatalf("unexpected anonymous name format: %q", name)
	}
}

func TestNormalizeUsername(t *testing.T) {
	got := normalizeUsername("  test-user  ")
	if got != "test-user" {
		t.Fatalf("normalizeUsername() = %q, want %q", got, "test-user")
	}
}

func TestIsValidUsername(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "too short", input: "ab", want: false},
		{name: "too long", input: "123456789012345678901", want: false},
		{name: "valid ascii", input: "user_01", want: true},
		{name: "valid unicode", input: "Иван-7", want: true},
		{name: "invalid symbol", input: "bad!name", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isValidUsername(tc.input)
			if got != tc.want {
				t.Fatalf("isValidUsername(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSanitizeForTerminal(t *testing.T) {
	input := "\x1b[31mRed\x1b[0m\nline\t\x00text"
	got := sanitizeForTerminal(input)
	want := "Red\nline\ttext"
	if got != want {
		t.Fatalf("sanitizeForTerminal() = %q, want %q", got, want)
	}
}
