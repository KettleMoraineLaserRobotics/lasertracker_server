package ws

import (
	"context"
	"encoding/json"
	"lasertracker_server/internal"
	"sync"

	"github.com/coder/websocket"
)

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
	broadcast  chan internal.Message
	direct     chan directMessage
	mu         sync.RWMutex
}

type directMessage struct {
	client *Client
	data   []byte
}

func NewHub() *Hub {
	return &Hub{
		groups:     make(map[string]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan internal.Message, 256),
		direct:     make(chan directMessage, 256),
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

		case dm := <-h.direct:
			h.mu.Lock()
			if h.groups[dm.client.GroupKey][dm.client] {
				select {
				case dm.client.Send <- dm.data:
				default:
					h.evict(dm.client)
				}
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			h.mu.Lock()
			clients := h.groups[event.GroupKey]
			data, err := json.Marshal(event)
			if err == nil {
				for client := range clients {
					select {
					case client.Send <- data:
					default:
						h.evict(client)
					}
				}
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) Broadcast(event internal.Message) {
	h.broadcast <- event
}

func (h *Hub) evict(c *Client) {
	clients := h.groups[c.GroupKey]
	if !clients[c] {
		return
	}
	delete(clients, c)
	close(c.Send)
	if len(clients) == 0 {
		delete(h.groups, c.GroupKey)
	}
	go c.Conn.Close(websocket.StatusPolicyViolation, "slow client")
}

func (h *Hub) SendTo(c *Client, data []byte) {
	h.direct <- directMessage{client: c, data: data}
}
