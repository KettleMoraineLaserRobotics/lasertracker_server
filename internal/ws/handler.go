package ws

import (
	"context"
	"encoding/json"
	"lasertracker_server/internal"
	"lasertracker_server/internal/actions"
	"lasertracker_server/internal/auth"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

func ServeWS(hub *Hub, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("token")
		claims, err := auth.ValidateToken(tokenStr, r.Context())
		if err != nil {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{
				"lasertracker.laserrobotics.org",
			},
		})
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
		_, messageBytes, err := c.Conn.Read(ctx)
		if err != nil {
			break
		}

		var msg internal.Message
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			log.Printf("Invalid message format from user %s: %v", c.Username, err)
			continue
		}

		c.handleInboundMessage(ctx, hub, msg)
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

func (c *Client) handleInboundMessage(ctx context.Context, hub *Hub, msg internal.Message) {
	switch msg.InfoType {
	case "UPDATE_MEMBER":
		c.handleUpdateMember(ctx, hub, msg.Payload)
	case "CHANGE_PIN":
		c.handleUpdateMemberPin(ctx, msg.Payload)
	case "UPDATE_BATTERY":
		c.handleUpdateBattery(ctx, hub, msg.Payload)
	case "UPDATE_GROUP_EVENT_KEY":
		c.handleUpdateGroupEventKey(ctx, hub, msg.Payload)
	case "ADD_BATTERY":
		c.handleAddBattery(ctx, hub, msg.Payload)
	case "REMOVE_BATTERY":
		c.handleRemoveBattery(ctx, hub, msg.Payload)
	}
}

func (c *Client) handleUpdateMember(ctx context.Context, hub *Hub, rawPayload json.RawMessage) {
	var payload internal.PublicMember
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		log.Printf("Invalid UPDATE_MEMBER payload from %s: %v", c.Username, err)
		return
	}

	payload.GroupKey = c.GroupKey

	sender, err := actions.GetMember(ctx, c.GroupKey, c.Username)
	if err != nil {
		log.Printf("Failed to fetch requesting member %s: %v", c.Username, err)
		return
	}

	target, err := actions.GetMember(ctx, c.GroupKey, payload.Username)
	if err != nil {
		log.Printf("Target member %s not found in group %s: %v", payload.Username, c.GroupKey, err)
		return
	}

	isSelf := c.Username == payload.Username
	if !isSelf && !sender.IsAdmin {
		log.Printf("Unauthorized UPDATE_MEMBER attempt by user %s on %s", c.Username, payload.Username)
		return
	}

	if !sender.IsAdmin {
		payload.IsAdmin = target.IsAdmin
	}

	err = actions.UpdateMember(ctx, payload)
	if err != nil {
		log.Printf("Error updating member %s in DB: %v", payload.Username, err)
		return
	}

	updatedPayload, _ := json.Marshal(payload)

	hub.Broadcast(internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), InfoType: "MEMBER_UPDATED", Payload: updatedPayload})
}

func (c *Client) handleUpdateBattery(ctx context.Context, hub *Hub, rawPayload json.RawMessage) {
	var payload internal.Battery
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		log.Printf("Invalid UPDATE_BATTERY payload from %s: %v", c.Username, err)
		return
	}

	payload.GroupKey = c.GroupKey

	_, err := actions.GetMember(ctx, c.GroupKey, c.Username)
	if err != nil {
		log.Printf("Failed to fetch requesting member %s: %v", c.Username, err)
		return
	}

	_, err = actions.GetBattery(ctx, c.GroupKey, payload.Name)
	if err != nil {
		log.Printf("Target battery %s not found in group %s: %v", payload.Name, c.GroupKey, err)
		return
	}

	err = actions.UpdateBattery(ctx, payload)
	if err != nil {
		log.Printf("Error updating battery %s in DB: %v", payload.Name, err)
		return
	}

	updatedPayload, _ := json.Marshal(payload)

	hub.Broadcast(internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), InfoType: "BATTERY_UPDATED", Payload: updatedPayload})
}

func (c *Client) handleUpdateMemberPin(ctx context.Context, rawPayload json.RawMessage) {
	var payload internal.PinChangeRequest
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		log.Printf("Invalid CHANGE_PIN payload from %s: %v", c.Username, err)
		return
	}

	payload.GroupKey = c.GroupKey

	sender, err := actions.GetMember(ctx, c.GroupKey, c.Username)
	if err != nil {
		log.Printf("Failed to fetch requesting member %s: %v", c.Username, err)
		return
	}

	_, err = actions.GetMember(ctx, c.GroupKey, payload.Username)
	if err != nil {
		log.Printf("Target member %s not found in group %s: %v", payload.Username, c.GroupKey, err)
		return
	}

	isSelf := c.Username == payload.Username
	if !isSelf && !sender.IsAdmin {
		log.Printf("Unauthorized CHANGE_PIN attempt by user %s on %s", c.Username, payload.Username)
		return
	}

	err = actions.ChangePin(ctx, payload)
	if err != nil {
		log.Printf("Error updating member %s in DB: %v", payload.Username, err)
		return
	}
}

func (c *Client) handleUpdateGroupEventKey(ctx context.Context, hub *Hub, rawPayload json.RawMessage) {
	var payload internal.Group
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		log.Printf("Invalid UPDATE_GROUP_EVENT_KEY payload from %s: %v", c.Username, err)
		return
	}

	payload.GroupKey = c.GroupKey
	sender, err := actions.GetMember(ctx, c.GroupKey, c.Username)
	if err != nil {
		log.Printf("Failed to fetch requesting member %s: %v", c.Username, err)
		return
	}
	if !sender.IsAdmin {
		log.Printf("Unauthorized UPDATE_GROUP_EVENT_KEY attempt by user %s", c.Username)
		return
	}

	if err := actions.ChangeGroupEventKey(ctx, payload.GroupKey, payload.EventKey); err != nil {
		log.Printf("Error updating group event key for group %s: %v", c.GroupKey, err)
		return
	}

	updatedPayload, _ := json.Marshal(payload)
	hub.Broadcast(internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), InfoType: "GROUP_EVENT_KEY_UPDATED", Payload: updatedPayload})
}

func (c *Client) handleAddBattery(ctx context.Context, hub *Hub, rawPayload json.RawMessage) {
	var payload internal.Battery
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		log.Printf("Invalid ADD_BATTERY payload from %s: %v", c.Username, err)
		return
	}

	payload.GroupKey = c.GroupKey
	if _, err := actions.GetMember(ctx, c.GroupKey, c.Username); err != nil {
		log.Printf("Failed to fetch requesting member %s: %v", c.Username, err)
		return
	}

	if err := actions.AddBattery(ctx, payload); err != nil {
		log.Printf("Error adding battery %s in DB: %v", payload.Name, err)
		return
	}

	updatedPayload, _ := json.Marshal(payload)
	hub.Broadcast(internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), InfoType: "BATTERY_ADDED", Payload: updatedPayload})
}

func (c *Client) handleRemoveBattery(ctx context.Context, hub *Hub, rawPayload json.RawMessage) {
	var payload internal.Battery
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		log.Printf("Invalid REMOVE_BATTERY payload from %s: %v", c.Username, err)
		return
	}

	payload.GroupKey = c.GroupKey
	if _, err := actions.GetMember(ctx, c.GroupKey, c.Username); err != nil {
		log.Printf("Failed to fetch requesting member %s: %v", c.Username, err)
		return
	}
	if _, err := actions.GetBattery(ctx, c.GroupKey, payload.Name); err != nil {
		log.Printf("Target battery %s not found in group %s: %v", payload.Name, c.GroupKey, err)
		return
	}

	if err := actions.RemoveBattery(ctx, payload.GroupKey, payload.Name); err != nil {
		log.Printf("Error removing battery %s from DB: %v", payload.Name, err)
		return
	}

	updatedPayload, _ := json.Marshal(payload)
	hub.Broadcast(internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), InfoType: "BATTERY_REMOVED", Payload: updatedPayload})
}
