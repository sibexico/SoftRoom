package main

import (
	"fmt"
	"strings"
)

func handleCommand(c *Client, input string) (Message, bool) {
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
			"  /w <user> <message>   - Send a private message"
		responseMsg = systemMessage(helpMsg)

	case "/u":
		users := c.hub.getUserList()
		userListMsg := fmt.Sprintf("Users online (%d): %s", len(users), strings.Join(users, ", "))
		responseMsg = systemMessage(userListMsg)

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
