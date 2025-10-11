package main

import (
	"log"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gliderlabs/ssh"
)

type Client struct {
	hub      *Hub
	user     string // Username
	isAuthed bool   // True if authenticated via GitHub
	session  ssh.Session
	send     chan Message
	program  *tea.Program // BubbleTea instance.
	mu       sync.RWMutex
}

func NewClient(session ssh.Session, hub *Hub, user string) *Client {
	return &Client{
		hub:      hub,
		user:     user,
		isAuthed: false, // Users start as anonymous
		session:  session,
		send:     make(chan Message, 256),
	}
}

func (c *Client) User() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.user
}

func (c *Client) SetUser(user string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.user = user
}

func (c *Client) IsAuthed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isAuthed
}

func (c *Client) SetIsAuthed(isAuthed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isAuthed = isAuthed
}

func (c *Client) RunTUI(width, height int, welcomeMsg string, cfg *Config) {
	model := initialModel(c, width, height, welcomeMsg, cfg)
	c.program = tea.NewProgram(
		model,
		tea.WithInput(c.session),
		tea.WithOutput(c.session),
		tea.WithAltScreen(),
	)

	go c.writePump()

	if _, err := c.program.Run(); err != nil {
		log.Printf("Error running TUI for %s: %v", c.User(), err)
	}

	c.session.Close()
}

func (c *Client) writePump() {
	for msg := range c.send {
		c.program.Send(incomingMessageMsg(msg))
	}
}