package ws

import (
	"context"
	"lasertracker_server/internal/auth"
	"net/http"

	"github.com/coder/websocket"
)

func ServeWS(hub *Hub, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("token")
		claims, err := auth.ValidateToken(tokenStr, jwtSecret)
		if err != nil {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			return
		}

		client := &Client{
			GroupKey: claims.GroupKey,
			Username: claims.Username,
			Conn:     conn,
			Send:     make(chan []byte, 256),
		}

		hub.register <- client

		go client.writePump(r.Context())
		go client.readPump(r.Context(), hub)
	}
}

func (c *Client) readPump(ctx context.Context, hub *Hub) {
	defer func() {
		hub.unregister <- c
		c.Conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		_, _, err := c.Conn.Read(ctx)
		if err != nil {
			break
		}
	}
}

func (c *Client) writePump(ctx context.Context) {
	defer c.Conn.Close(websocket.StatusNormalClosure, "")

	for {
		select {
		case msg, ok := <-c.Send:
			if !ok {
				c.Conn.Close(websocket.StatusGoingAway, "Channel Closed")
				return
			}
			err := c.Conn.Write(ctx, websocket.MessageText, msg)
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
