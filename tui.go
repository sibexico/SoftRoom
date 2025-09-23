package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type incomingMessageMsg Message
type errMsg error

type tuiModel struct {
	client       *Client
	viewport     viewport.Model
	textarea     textarea.Model
	senderStyle  lipgloss.Style // Chat message nicknames style (authenticated)
	anonStyle    lipgloss.Style // Nickname style for anonymous users
	systemStyle  lipgloss.Style // System messages
	whisperStyle lipgloss.Style // Private messages
	errorStyle   lipgloss.Style
	err          error
	welcome      string
	config       *Config
}

// The initial state of the TUI.
func initialModel(client *Client, width, height int, welcomeMsg string, cfg *Config) tuiModel {
	ta := textarea.New()
	ta.Placeholder = "Send a message... (/h for help)"
	ta.Focus()
	ta.CharLimit = 280
	ta.SetHeight(3)
	ta.SetWidth(width)

	vp := viewport.New(width, height-ta.Height())
	vp.SetContent("Welcome to SoftRoom!") // Generic welcome, specific one is sent via system message

	return tuiModel{
		client:       client,
		textarea:     ta,
		viewport:     vp,
		senderStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("2")),   // Green
		anonStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("240")), // Gray
		systemStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("11")),  // Yellow
		whisperStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("13")),  // Magenta
		errorStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("9")),   // Red
		welcome:      welcomeMsg,
		config:       cfg,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	m.textarea, tiCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			input := strings.TrimSpace(m.textarea.Value())
			if input == "" {
				m.textarea.Reset()
				return m, nil
			}
			m.textarea.Reset()

			responseMsg, isCmd := handleCommand(m.client, input, m.config)
			if isCmd {
				if responseMsg.Content != "" {
					return m, func() tea.Msg {
						return incomingMessageMsg(responseMsg)
					}
				}
				return m, nil
			}

			m.client.hub.broadcast <- Message{
				Author:         m.client.user,
				Content:        input,
				Type:           "public",
				AuthorIsAuthed: m.client.isAuthed,
			}
			return m, nil
		}

	case incomingMessageMsg:
		var newContent string
		switch msg.Type {
		case "private":
			newContent = m.whisperStyle.Render(fmt.Sprintf("[%s] %s", time.Now().Format("15:04"), msg.Content))
		case "system":
			newContent = m.systemStyle.Render(fmt.Sprintf("[%s] %s", time.Now().Format("15:04"), msg.Content))
		case "public":
			fallthrough
		default:
			var author string
			if msg.AuthorIsAuthed {
				author = m.senderStyle.Render(msg.Author)
			} else {
				author = m.anonStyle.Render(fmt.Sprintf("[anon] %s", msg.Author))
			}
			newContent = fmt.Sprintf("[%s] %s: %s", time.Now().Format("15:04"), author, msg.Content)
		}

		m.viewport.SetContent(m.viewport.View() + "\n" + newContent)
		m.viewport.GotoBottom()
		return m, nil

	case errMsg:
		m.err = msg
		return m, nil

	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - m.textarea.Height()
		m.textarea.SetWidth(msg.Width)
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m tuiModel) View() string {
	return fmt.Sprintf(
		"%s\n%s",
		m.viewport.View(),
		m.textarea.View(),
	)
}
