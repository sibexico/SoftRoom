package main

import "testing"

func TestEnvValueAndFirstLocaleEnv(t *testing.T) {
	env := []string{
		"LANG=en_US.UTF-8",
		"LC_CTYPE=ru_RU.KOI8-R",
	}

	if got := envValue(env, "LANG"); got != "en_US.UTF-8" {
		t.Fatalf("envValue LANG = %q", got)
	}

	if got := firstLocaleEnv(env); got != "ru_RU.KOI8-R" {
		t.Fatalf("firstLocaleEnv = %q, want %q", got, "ru_RU.KOI8-R")
	}
}

func TestExtractLocaleCharset(t *testing.T) {
	tests := []struct {
		locale string
		want   string
	}{
		{locale: "en_US.UTF-8", want: "UTF-8"},
		{locale: "ru_RU.KOI8-R@foo", want: "KOI8-R"},
		{locale: "utf-8", want: "utf-8"},
		{locale: "", want: ""},
		{locale: "en_US", want: ""},
	}

	for _, tc := range tests {
		if got := extractLocaleCharset(tc.locale); got != tc.want {
			t.Fatalf("extractLocaleCharset(%q) = %q, want %q", tc.locale, got, tc.want)
		}
	}
}

func TestUTF8AndCharsetNormalization(t *testing.T) {
	if !isUTF8Charset("UTF-8") {
		t.Fatal("isUTF8Charset should accept UTF-8")
	}

	if got := normalizeCharsetName("cp1251"); got != "windows-1251" {
		t.Fatalf("normalizeCharsetName(cp1251) = %q", got)
	}

	if got := normalizeCharsetName("koi8u"); got != "koi8-u" {
		t.Fatalf("normalizeCharsetName(koi8u) = %q", got)
	}
}
