package main

import (
	"fmt"
	"log"
	"sync"
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

type remoteNameChangeRequest struct {
	oldName      string
	newName      string
	isGitHubAuth bool
	serverAddr   string
}

type nickSyncRequest struct {
	serverAddr string
	nicks      []string
}

type Hub struct {
	mu                sync.RWMutex
	clients           map[*Client]bool
	clientsByName     map[string]*Client
	remoteNicks       map[string][]string
	broadcast         chan Message
	register          chan *Client
	unregister        chan *Client
	requestUsers      chan chan []string
	requestLocalUsers chan chan []string
	privateMsgChan    chan privateMessagePayload
	changeName        chan nameChangeRequest
	remoteNameChange  chan remoteNameChangeRequest
	syncNicks         chan nickSyncRequest
	federation        *Federation
}

func newHub() *Hub {
	return &Hub{
		broadcast:         make(chan Message),
		register:          make(chan *Client),
		unregister:        make(chan *Client),
		clients:           make(map[*Client]bool),
		clientsByName:     make(map[string]*Client),
		remoteNicks:       make(map[string][]string),
		requestUsers:      make(chan chan []string),
		requestLocalUsers: make(chan chan []string),
		privateMsgChan:    make(chan privateMessagePayload),
		changeName:        make(chan nameChangeRequest),
		remoteNameChange:  make(chan remoteNameChangeRequest),
		syncNicks:         make(chan nickSyncRequest),
	}
}

func (h *Hub) getUserList() []string {
	respChan := make(chan []string)
	h.requestUsers <- respChan
	return <-respChan
}

func (h *Hub) getLocalUserList() []string {
	respChan := make(chan []string)
	h.requestLocalUsers <- respChan
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

func (h *Hub) findServerForNick(nick string) (string, bool) {
	for serverAddr, nicks := range h.remoteNicks {
		for _, n := range nicks {
			if n == nick {
				return serverAddr, true
			}
		}
	}
	return "", false
}

func (h *Hub) isNameTakenInFederation(name string) bool {
	// Check local clients
	if _, exists := h.clientsByName[name]; exists {
		return true
	}

	// Check remote users
	for _, nicks := range h.remoteNicks {
		for _, nick := range nicks {
			if nick == name {
				return true
			}
		}
	}
	return false
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
			finalName := client.User()
			for _, exists := h.clientsByName[finalName]; exists; _, exists = h.clientsByName[finalName] {
				finalName = generateAnonymousName()
			}
			client.SetUser(finalName)

			h.clients[client] = true
			h.clientsByName[client.User()] = client
			log.Printf("Client registered: %s", client.User())
			joinMsg := Message{Author: "System", Content: client.User() + " has joined.", Type: "system"}
			for c := range h.clients {
				select {
				case c.send <- joinMsg:
				default:
					log.Printf("client %s send channel full, disconnecting", c.User())
					close(c.send)
					delete(h.clients, c)
					delete(h.clientsByName, c.User())
				}
			}

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				delete(h.clientsByName, client.User())
				close(client.send)
				log.Printf("Client unregistered: %s", client.User())
				leaveMsg := Message{Author: "System", Content: client.User() + " has left.", Type: "system"}
				for c := range h.clients {
					c.send <- leaveMsg
				}
			}

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					log.Printf("client %s send channel full, disconnecting", client.User())
					close(client.send)
					delete(h.clients, client)
					delete(h.clientsByName, client.User())
				}
			}
			h.mu.RUnlock()

		case respChan := <-h.requestUsers:
			var users []string
			for client := range h.clients {
				users = append(users, client.User())
			}
			for _, nicks := range h.remoteNicks {
				users = append(users, nicks...)
			}
			respChan <- users

		case respChan := <-h.requestLocalUsers:
			h.mu.RLock()
			var users []string
			for client := range h.clients {
				users = append(users, client.User())
			}
			respChan <- users
			h.mu.RUnlock()

		case pMsg := <-h.privateMsgChan:
			h.mu.RLock()
			targetClient, found := h.clientsByName[pMsg.TargetUser]
			if found {
				if pMsg.Sender != nil && targetClient == pMsg.Sender {
					pMsg.Sender.send <- Message{Type: "system", Content: "You can't send a message to yourself."}
					continue
				}

				targetMsg := Message{
					Type:    "private",
					Content: fmt.Sprintf("(from %s): %s", pMsg.Message.Author, pMsg.Message.Content),
				}
				targetClient.send <- targetMsg

				if pMsg.Sender != nil {
					senderConfirmMsg := Message{
						Type:    "private",
						Content: fmt.Sprintf("(to %s): %s", pMsg.TargetUser, pMsg.Message.Content),
					}
					pMsg.Sender.send <- senderConfirmMsg
				}
			} else {
				// Check remote users
				foundRemote := false
				for serverAddr, nicks := range h.remoteNicks {
					for _, nick := range nicks {
						if nick == pMsg.TargetUser {
							for _, server := range h.federation.servers {
								if server.addr == serverAddr {
									server.sendPrivateMessage(pMsg.Message.Author, pMsg.TargetUser, pMsg.Message.Content)
									foundRemote = true
									break
								}
							}
						}
						if foundRemote {
							break
						}
					}
					if foundRemote {
						break
					}
				}
				if !foundRemote && pMsg.Sender != nil {
					pMsg.Sender.send <- Message{Type: "system", Content: fmt.Sprintf("User '%s' not found.", pMsg.TargetUser)}
				}
			}
			h.mu.RUnlock()

		case req := <-h.changeName:
			h.mu.Lock()
			nameTakenInFederation := h.isNameTakenInFederation(req.newName)
			existingClient, nameTakenLocally := h.clientsByName[req.newName]
			h.mu.Unlock()

			if (nameTakenInFederation && req.client.User() != req.newName) || (nameTakenLocally && existingClient != req.client) {
				if req.isGitHubAuth {
					// GitHub auth takes precedence. Kick the existing user off the name.
					kickedUserOldName := existingClient.User()
					newAnonName := generateAnonymousName()
					for _, exists := h.clientsByName[newAnonName]; exists; _, exists = h.clientsByName[newAnonName] {
						newAnonName = generateAnonymousName()
					}

					// Update the kicked user's details
					delete(h.clientsByName, kickedUserOldName)
					h.clientsByName[newAnonName] = existingClient
					existingClient.SetUser(newAnonName)
					existingClient.SetIsAuthed(false) // Reset auth status
					existingClient.send <- SystemMessage(fmt.Sprintf("Your name was changed to %s because an authenticating user claimed the name '%s'.", newAnonName, kickedUserOldName))

					// Now the name is free, proceed to update the authenticating user
					oldAuthName := req.client.User()
					delete(h.clientsByName, oldAuthName)
					h.clientsByName[req.newName] = req.client
					req.client.SetUser(req.newName)
					req.client.SetIsAuthed(true) // Set auth status

					// Broadcast both changes
					broadcastMsg1 := SystemMessage(fmt.Sprintf("%s has been renamed to %s.", kickedUserOldName, newAnonName))
					broadcastMsg2 := SystemMessage(fmt.Sprintf("%s has authenticated and is now known as %s.", oldAuthName, req.newName))
					for c := range h.clients {
						c.send <- broadcastMsg1
						c.send <- broadcastMsg2
					}

					// Notify federation about the changes
					h.federation.BroadcastNameChange(kickedUserOldName, newAnonName, false)
					h.federation.BroadcastNameChange(oldAuthName, req.newName, true)

				} else {
					// Normal name change, name is taken. Reject.
					req.client.send <- SystemMessage(fmt.Sprintf("Name '%s' is already taken.", req.newName))
				}
			} else {
				// Name is not taken or user is re-setting their own name.
				oldName := req.client.User()
				if oldName == req.newName {
					continue // No change
				}

				delete(h.clientsByName, oldName)
				h.clientsByName[req.newName] = req.client
				req.client.SetUser(req.newName)

				// If user is just renaming, they lose their GitHub auth status unless it's their GitHub name
				if !req.isGitHubAuth {
					req.client.SetIsAuthed(false)
				} else {
					req.client.SetIsAuthed(true)
				}

				// Broadcast the change.
				broadcastMsg := SystemMessage(fmt.Sprintf("%s is now known as %s.", oldName, req.newName))
				for c := range h.clients {
					c.send <- broadcastMsg
				}

				h.federation.BroadcastNameChange(oldName, req.newName, req.isGitHubAuth)
			}
		case req := <-h.syncNicks:
			h.mu.Lock()
			// Check for name conflicts before updating
			for _, newNick := range req.nicks {
				// Skip if the nick is from the same server we're updating
				if currentServer, exists := h.findServerForNick(newNick); exists && currentServer != req.serverAddr {
					log.Printf("Warning: User %s exists on multiple servers (%s and %s)", newNick, currentServer, req.serverAddr)
				}
			}
			h.remoteNicks[req.serverAddr] = req.nicks
			h.mu.Unlock()

		case req := <-h.remoteNameChange:
			h.mu.Lock()
			for i, nick := range h.remoteNicks[req.serverAddr] {
				if nick == req.oldName {
					h.remoteNicks[req.serverAddr] = append(h.remoteNicks[req.serverAddr][:i], h.remoteNicks[req.serverAddr][i+1:]...)
					break
				}
			}
			// Add the new nick
			h.remoteNicks[req.serverAddr] = append(h.remoteNicks[req.serverAddr], req.newName)

			// If github auth, check for local users with the same name
			if req.isGitHubAuth {
				if client, ok := h.clientsByName[req.newName]; ok {
					// Kick the local user
					kickedUserOldName := client.User()
					newAnonName := generateAnonymousName()
					for _, exists := h.clientsByName[newAnonName]; exists; _, exists = h.clientsByName[newAnonName] {
						newAnonName = generateAnonymousName()
					}
					delete(h.clientsByName, kickedUserOldName)
					h.clientsByName[newAnonName] = client
					client.SetUser(newAnonName)
					client.SetIsAuthed(false)
					client.send <- SystemMessage(fmt.Sprintf("Your name was changed to %s because an authenticating user claimed the name '%s'.", newAnonName, kickedUserOldName))
					h.federation.BroadcastNameChange(kickedUserOldName, newAnonName, false)
				}
			}
			h.mu.Unlock()
		}
	}
}
