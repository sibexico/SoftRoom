package main

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func generateAnonymousName() string {
	var b [2]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return "Anonymous0000"
	}

	return fmt.Sprintf("Anonymous%04d", int(binary.BigEndian.Uint16(b[:]))%10000)
}

func normalizeUsername(name string) string {
	return norm.NFC.String(strings.TrimSpace(name))
}

func isValidUsername(name string) bool {
	normalized := normalizeUsername(name)
	runeCount := utf8.RuneCountInString(normalized)
	if runeCount < 3 || runeCount > 20 {
		return false
	}

	for _, r := range normalized {
		if r == '_' || r == '-' {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
			continue
		}
		return false
	}

	return true
}

func sanitizeForTerminal(input string) string {
	cleaned := ansiEscapePattern.ReplaceAllString(input, "")

	var b strings.Builder
	b.Grow(len(cleaned))

	for _, r := range cleaned {
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}

	return b.String()
}
