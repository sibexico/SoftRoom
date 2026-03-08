package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

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
