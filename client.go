package main

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gliderlabs/ssh"
)

type Client struct {
	hub     *Hub
	user    string // GitHub username
	session ssh.Session
	send    chan Message
	program *tea.Program // BubbleTea instance.

}

func NewClient(session ssh.Session, hub *Hub, user string) *Client {
	return &Client{
		hub:     hub,
		user:    user,
		session: session,
		send:    make(chan Message, 256),
	}
}

func (c *Client) RunTUI(width, height int, welcomeMsg string) {
	model := initialModel(c, width, height, welcomeMsg)
	c.program = tea.NewProgram(
		model,
		tea.WithInput(c.session),
		tea.WithOutput(c.session),
		tea.WithAltScreen(),
	)

	go c.writePump()

	if _, err := c.program.Run(); err != nil {
		log.Printf("Error running TUI for %s: %v", c.user, err)
	}

	c.session.Close()
}

func (c *Client) writePump() {
	for msg := range c.send {
		c.program.Send(incomingMessageMsg(msg))
	}
}
