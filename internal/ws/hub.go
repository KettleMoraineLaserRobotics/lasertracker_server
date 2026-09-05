package ws

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/coder/websocket"
)

type Message struct {
	InfoType string
	GroupKey string
	Payload  json.RawMessage
}

type Client struct {
	GroupKey string
	Username string
	Conn     *websocket.Conn
	Send     chan []byte
}

type Hub struct {
	groups     map[string]map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan Message
	mu         sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		groups:     make(map[string]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Message, 256),
	}
}

func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case client := <-h.register:
			h.mu.Lock()
			if h.groups[client.GroupKey] == nil {
				h.groups[client.GroupKey] = make(map[*Client]bool)
			}
			h.groups[client.GroupKey][client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.groups[client.GroupKey]; ok {
				if _, exists := clients[client]; exists {
					delete(clients, client)
					close(client.Send)
					if len(clients) == 0 {
						delete(h.groups, client.GroupKey)
					}
				}
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			h.mu.RLock()
			clients := h.groups[event.GroupKey]
			data, err := json.Marshal(event)
			if err == nil {
				for client := range clients {
					select {
					case client.Send <- data:
					default:
						close(client.Send)
						delete(clients, client)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Broadcast(event Message) {
	h.broadcast <- event
}
