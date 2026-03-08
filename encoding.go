package main

import (
	"io"
	"strings"

	"github.com/gliderlabs/ssh"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

type sessionIO struct {
	input        io.Reader
	output       io.Writer
	charset      string
	charsetKnown bool
}

func buildSessionIO(s ssh.Session) sessionIO {
	locale := firstLocaleEnv(s.Environ())
	charset := extractLocaleCharset(locale)

	if charset == "" || isUTF8Charset(charset) {
		return sessionIO{input: s, output: s, charset: "utf-8", charsetKnown: true}
	}

	encodingName := normalizeCharsetName(charset)
	enc, err := htmlindex.Get(encodingName)
	if err != nil {
		return sessionIO{input: s, output: s, charset: charset, charsetKnown: false}
	}

	return sessionIO{
		input:        transform.NewReader(s, enc.NewDecoder()),
		output:       transform.NewWriter(s, enc.NewEncoder()),
		charset:      encodingName,
		charsetKnown: true,
	}
}

func firstLocaleEnv(env []string) string {
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if value := envValue(env, key); value != "" {
			return value
		}
	}
	return ""
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(entry, prefix))
		}
	}
	return ""
}

func extractLocaleCharset(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return ""
	}

	if idx := strings.Index(locale, "@"); idx >= 0 {
		locale = locale[:idx]
	}

	if idx := strings.Index(locale, "."); idx >= 0 && idx+1 < len(locale) {
		return strings.TrimSpace(locale[idx+1:])
	}

	if !strings.ContainsAny(locale, "_=,") {
		return locale
	}

	return ""
}

func isUTF8Charset(charset string) bool {
	normalized := strings.ToLower(strings.TrimSpace(charset))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	return normalized == "utf8"
}

func normalizeCharsetName(charset string) string {
	normalized := strings.ToLower(strings.TrimSpace(charset))
	normalized = strings.ReplaceAll(normalized, "_", "-")

	switch normalized {
	case "cp1251", "windows1251":
		return "windows-1251"
	case "cp1252", "windows1252":
		return "windows-1252"
	case "cp866", "ibm866", "866":
		return "ibm866"
	case "koi8r":
		return "koi8-r"
	case "koi8u":
		return "koi8-u"
	default:
		return normalized
	}
}
