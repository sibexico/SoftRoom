package main

import (
	"io"

	"github.com/charmbracelet/ssh"
)

type sessionIO struct {
	input        io.Reader
	output       io.Writer
	charset      string
	charsetKnown bool
}

const defaultSessionCharset = "utf-8"

func buildSessionIO(s ssh.Session) sessionIO {
	return sessionIO{
		input:        s,
		output:       s,
		charset:      defaultSessionCharset,
		charsetKnown: true,
	}
}
