package main

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func generateAnonymousName() string {
	return fmt.Sprintf("Anonymous%04d", rand.Intn(10000))
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
