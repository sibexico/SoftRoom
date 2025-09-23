package main

import (
	"fmt"
	"log"
)

type Message struct {
	Author         string
	Content        string
	Type           string // "public", "private", "system"
	AuthorIsAuthed bool   // True if the author is authenticated
}

type privateMessagePayload struct {
	TargetUser string
	Message    Message
	Sender     *Client
}

type nameChangeRequest struct {
	client       *Client
	newName      string
	isGitHubAuth bool // Flag to give priority
}

type Hub struct {
	clients        map[*Client]bool
	clientsByName  map[string]*Client
	broadcast      chan Message
	register       chan *Client
	unregister     chan *Client
	requestUsers   chan chan []string
	privateMsgChan chan privateMessagePayload
	changeName     chan nameChangeRequest
}

func newHub() *Hub {
	return &Hub{
		broadcast:      make(chan Message),
		register:       make(chan *Client),
		unregister:     make(chan *Client),
		clients:        make(map[*Client]bool),
		clientsByName:  make(map[string]*Client),
		requestUsers:   make(chan chan []string),
		privateMsgChan: make(chan privateMessagePayload),
		changeName:     make(chan nameChangeRequest),
	}
}

func (h *Hub) getUserList() []string {
	respChan := make(chan []string)
	h.requestUsers <- respChan
	return <-respChan
}

func (h *Hub) sendPrivateMessage(targetUser string, msg Message, sender *Client) {
	payload := privateMessagePayload{
		TargetUser: targetUser,
		Message:    msg,
		Sender:     sender,
	}
	h.privateMsgChan <- payload
}

func (h *Hub) requestNameChange(client *Client, newName string, isGitHubAuth bool) {
	h.changeName <- nameChangeRequest{
		client:       client,
		newName:      newName,
		isGitHubAuth: isGitHubAuth,
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			// Ensure the initial anonymous name doesn't conflict
			finalName := client.user
			for _, exists := h.clientsByName[finalName]; exists; _, exists = h.clientsByName[finalName] {
				finalName = generateAnonymousName()
			}
			client.user = finalName

			h.clients[client] = true
			h.clientsByName[client.user] = client
			log.Printf("Client registered: %s", client.user)
			joinMsg := Message{Author: "System", Content: client.user + " has joined.", Type: "system"}
			for c := range h.clients {
				select {
				case c.send <- joinMsg:
				default:
					close(c.send)
					delete(h.clients, c)
					delete(h.clientsByName, c.user)
				}
			}

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				delete(h.clientsByName, client.user)
				close(client.send)
				log.Printf("Client unregistered: %s", client.user)
				leaveMsg := Message{Author: "System", Content: client.user + " has left.", Type: "system"}
				for c := range h.clients {
					c.send <- leaveMsg
				}
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
					delete(h.clientsByName, client.user)
				}
			}

		case respChan := <-h.requestUsers:
			var users []string
			for client := range h.clients {
				users = append(users, client.user)
			}
			respChan <- users

		case pMsg := <-h.privateMsgChan:
			targetClient, found := h.clientsByName[pMsg.TargetUser]
			if !found {
				pMsg.Sender.send <- Message{Type: "system", Content: fmt.Sprintf("User '%s' not found.", pMsg.TargetUser)}
				continue
			}

			if targetClient == pMsg.Sender {
				pMsg.Sender.send <- Message{Type: "system", Content: "You can't send a message to yourself."}
				continue
			}

			targetMsg := Message{
				Type:    "private",
				Content: fmt.Sprintf("(from %s): %s", pMsg.Message.Author, pMsg.Message.Content),
			}
			targetClient.send <- targetMsg

			senderConfirmMsg := Message{
				Type:    "private",
				Content: fmt.Sprintf("(to %s): %s", pMsg.TargetUser, pMsg.Message.Content),
			}
			pMsg.Sender.send <- senderConfirmMsg

		case req := <-h.changeName:
			existingClient, nameTaken := h.clientsByName[req.newName]

			if nameTaken && existingClient != req.client {
				if req.isGitHubAuth {
					// GitHub auth takes precedence. Kick the existing user off the name.
					kickedUserOldName := existingClient.user
					newAnonName := generateAnonymousName()
					for _, exists := h.clientsByName[newAnonName]; exists; _, exists = h.clientsByName[newAnonName] {
						newAnonName = generateAnonymousName()
					}

					// Update the kicked user's details
					delete(h.clientsByName, kickedUserOldName)
					h.clientsByName[newAnonName] = existingClient
					existingClient.user = newAnonName
					existingClient.isAuthed = false // Reset auth status
					existingClient.send <- systemMessage(fmt.Sprintf("Your name was changed to %s because an authenticating user claimed the name '%s'.", newAnonName, kickedUserOldName))

					// Now the name is free, proceed to update the authenticating user
					oldAuthName := req.client.user
					delete(h.clientsByName, oldAuthName)
					h.clientsByName[req.newName] = req.client
					req.client.user = req.newName
					req.client.isAuthed = true // Set auth status

					// Broadcast both changes
					broadcastMsg1 := systemMessage(fmt.Sprintf("%s has been renamed to %s.", kickedUserOldName, newAnonName))
					broadcastMsg2 := systemMessage(fmt.Sprintf("%s has authenticated and is now known as %s.", oldAuthName, req.newName))
					for c := range h.clients {
						c.send <- broadcastMsg1
						c.send <- broadcastMsg2
					}

				} else {
					// Normal name change, name is taken. Reject.
					req.client.send <- systemMessage(fmt.Sprintf("Name '%s' is already taken.", req.newName))
				}
			} else {
				// Name is not taken or user is re-setting their own name.
				oldName := req.client.user
				if oldName == req.newName {
					continue // No change
				}

				delete(h.clientsByName, oldName)
				h.clientsByName[req.newName] = req.client
				req.client.user = req.newName

				// If user is just renaming, they lose their GitHub auth status unless it's their GitHub name
				if !req.isGitHubAuth {
					req.client.isAuthed = false
				} else {
					req.client.isAuthed = true
				}

				// Broadcast the change.
				broadcastMsg := systemMessage(fmt.Sprintf("%s is now known as %s.", oldName, req.newName))
				for c := range h.clients {
					c.send <- broadcastMsg
				}
			}
		}
	}
}
