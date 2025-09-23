package main

import (
	"fmt"
	"regexp"
	"strings"
)

var validUsernameRegex = regexp.MustCompile("^[a-zA-Z0-9_-]{3,20}$")

func handleCommand(c *Client, input string, cfg *Config) (Message, bool) {
	if !strings.HasPrefix(input, "/") {
		return Message{}, false
	}

	parts := strings.Fields(input)
	command := parts[0]
	var responseMsg Message

	switch command {
	case "/h":
		helpMsg := "Available commands:\n" +
			"  /h                    - Show this help message\n" +
			"  /u                    - List users in the chat\n" +
			"  /n <name>             - Change your name\n" +
			"  /w <user> <message>   - Send a private message\n" +
			"  /gh                   - Authenticate with GitHub to get your GitHub name"
		responseMsg = systemMessage(helpMsg)

	case "/u":
		users := c.hub.getUserList()
		userListMsg := fmt.Sprintf("Users online (%d): %s", len(users), strings.Join(users, ", "))
		responseMsg = systemMessage(userListMsg)

	case "/n":
		if len(parts) < 2 {
			responseMsg = systemMessage("Usage: /n <newname>")
		} else {
			newName := parts[1]
			if !validUsernameRegex.MatchString(newName) {
				responseMsg = systemMessage("Invalid name. Use 3-20 alphanumeric characters, underscores, or hyphens.")
			} else {
				c.hub.requestNameChange(c, newName, false)
				// The hub will send feedback directly to the client.
				return Message{}, true
			}
		}

	case "/gh":
		c.send <- systemMessage("Starting GitHub authentication...")
		go handleAuthentication(c, cfg)
		return Message{}, true

	case "/w":
		if len(parts) < 3 {
			responseMsg = systemMessage("Usage: /w <username> <message>")
		} else {
			targetUser := parts[1]
			content := strings.Join(parts[2:], " ")
			msg := Message{
				Author:  c.user,
				Content: content,
			}
			c.hub.sendPrivateMessage(targetUser, msg, c)
			return Message{}, true
		}

	default:
		responseMsg = systemMessage(fmt.Sprintf("Unknown command: %s", command))
	}

	return responseMsg, true
}

func systemMessage(content string) Message {
	return Message{
		Author:  "System",
		Content: content,
		Type:    "system",
	}
}
