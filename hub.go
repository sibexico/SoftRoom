package main

import (
	"fmt"
	"log"
)

type Message struct {
	Author  string
	Content string
	Type    string // "public", "private", "system"
}

type privateMessagePayload struct {
	TargetUser string
	Message    Message
	Sender     *Client
}

type Hub struct {
	clients        map[*Client]bool
	broadcast      chan Message
	register       chan *Client
	unregister     chan *Client
	requestUsers   chan chan []string
	privateMsgChan chan privateMessagePayload
}

func newHub() *Hub {
	return &Hub{
		broadcast:      make(chan Message),
		register:       make(chan *Client),
		unregister:     make(chan *Client),
		clients:        make(map[*Client]bool),
		requestUsers:   make(chan chan []string),
		privateMsgChan: make(chan privateMessagePayload),
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

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Client registered: %s", client.user)
			joinMsg := Message{Author: "System", Content: client.user + " has joined.", Type: "system"}
			for client := range h.clients {
				select {
				case client.send <- joinMsg:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
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
				}
			}

		case respChan := <-h.requestUsers:
			var users []string
			for client := range h.clients {
				users = append(users, client.user)
			}
			respChan <- users

		case pMsg := <-h.privateMsgChan:
			var targetClient *Client
			for client := range h.clients {
				if client.user == pMsg.TargetUser {
					targetClient = client
					break
				}
			}

			if targetClient == nil {
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

			// Send a confirmation to the sender
			senderConfirmMsg := Message{
				Type:    "private",
				Content: fmt.Sprintf("(to %s): %s", pMsg.TargetUser, pMsg.Message.Content),
			}
			pMsg.Sender.send <- senderConfirmMsg
		}
	}
}
