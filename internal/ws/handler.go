package ws

import (
	"context"
	"encoding/json"
	"lasertracker_server/internal"
	"lasertracker_server/internal/actions"
	"lasertracker_server/internal/auth"
	"lasertracker_server/internal/eventsync"
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
			OriginPatterns: []string{"*"},
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

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		go client.writePump(ctx)
		client.readPump(ctx, hub)
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
	var err error
	switch msg.InfoType {
	case "UPDATE_MEMBER":
		err = c.handleUpdateMember(ctx, hub, msg.Payload)
	case "CHANGE_PIN":
		err = c.handleUpdateMemberPin(ctx, msg.Payload)
	case "UPDATE_BATTERY":
		err = c.handleUpdateBattery(ctx, hub, msg.Payload)
	case "UPDATE_GROUP_EVENT_KEY":
		err = c.handleUpdateGroupEventKey(ctx, hub, msg.Payload)
	case "ADD_BATTERY":
		err = c.handleAddBattery(ctx, hub, msg.Payload)
	case "REMOVE_BATTERY":
		err = c.handleRemoveBattery(ctx, hub, msg.Payload)
	case "SYNC":
		if err = c.handleDataSync(ctx, hub, msg.RequestID); err == nil {
			return
		}
	default:
		err = &wsError{"unknown_type", "unknown info_type"}
	}

	if err != nil {
		log.Printf("%s from %s failed: %v", msg.InfoType, c.Username, err)
	}
	if msg.RequestID != "" {
		c.reply(hub, msg.RequestID, err)
	}
}

func (c *Client) reply(hub *Hub, requestID string, err error) {
	reply := internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), RequestID: requestID}
	if err == nil {
		reply.InfoType, reply.Payload = "ACK", json.RawMessage(`{}`)
	} else {
		we := toWSError(err)
		reply.InfoType = "ERROR"
		reply.Payload, _ = json.Marshal(map[string]string{"code": we.Code, "message": we.Message})
	}
	if data, mErr := json.Marshal(reply); mErr == nil {
		hub.SendTo(c, data)
	}
}

func (c *Client) handleUpdateMember(ctx context.Context, hub *Hub, rawPayload json.RawMessage) error {
	var payload internal.PublicMember
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return errInvalidPayload(err)
	}

	payload.GroupKey = c.GroupKey

	sender, err := actions.GetMember(ctx, c.GroupKey, c.Username)
	if err != nil {
		return err
	}

	target, err := actions.GetMember(ctx, c.GroupKey, payload.Username)
	if err != nil {
		return err
	}

	isSelf := c.Username == payload.Username
	if !isSelf && !sender.IsAdmin {
		return errForbidden
	}

	if !sender.IsAdmin {
		payload.IsAdmin = target.IsAdmin
	}

	err = actions.UpdateMember(ctx, payload)
	if err != nil {
		return err
	}

	updatedPayload, _ := json.Marshal(payload)

	hub.Broadcast(internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), InfoType: "MEMBER_UPDATED", Payload: updatedPayload})
	return nil
}

func (c *Client) handleUpdateBattery(ctx context.Context, hub *Hub, rawPayload json.RawMessage) error {
	var payload internal.Battery
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return errInvalidPayload(err)
	}

	payload.GroupKey = c.GroupKey

	_, err := actions.GetMember(ctx, c.GroupKey, c.Username)
	if err != nil {
		return err
	}

	_, err = actions.GetBattery(ctx, c.GroupKey, payload.Name)
	if err != nil {
		return err
	}

	err = actions.UpdateBattery(ctx, payload)
	if err != nil {
		return err
	}

	updatedPayload, _ := json.Marshal(payload)

	hub.Broadcast(internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), InfoType: "BATTERY_UPDATED", Payload: updatedPayload})
	return nil
}

func (c *Client) handleUpdateMemberPin(ctx context.Context, rawPayload json.RawMessage) error {
	var payload internal.PinChangeRequest
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return errInvalidPayload(err)
	}

	payload.GroupKey = c.GroupKey

	sender, err := actions.GetMember(ctx, c.GroupKey, c.Username)
	if err != nil {
		return err
	}

	_, err = actions.GetMember(ctx, c.GroupKey, payload.Username)
	if err != nil {
		return err
	}

	isSelf := c.Username == payload.Username
	if !isSelf && !sender.IsAdmin {
		return errForbidden
	}

	err = actions.ChangePin(ctx, payload)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) handleUpdateGroupEventKey(ctx context.Context, hub *Hub, rawPayload json.RawMessage) error {
	var payload internal.Group
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return errInvalidPayload(err)
	}

	payload.GroupKey = c.GroupKey
	sender, err := actions.GetMember(ctx, c.GroupKey, c.Username)
	if err != nil {
		return err
	}
	if !sender.IsAdmin {
		return errForbidden
	}

	if err := actions.ChangeGroupEventKey(ctx, payload.GroupKey, payload.EventKey); err != nil {
		return err
	}

	eventsync.RegisterEventInBackground(payload.EventKey)

	updatedPayload, _ := json.Marshal(payload)
	hub.Broadcast(internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), InfoType: "GROUP_EVENT_KEY_UPDATED", Payload: updatedPayload})
	return nil
}

func (c *Client) handleAddBattery(ctx context.Context, hub *Hub, rawPayload json.RawMessage) error {
	var payload internal.Battery
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return errInvalidPayload(err)
	}

	payload.GroupKey = c.GroupKey
	if _, err := actions.GetMember(ctx, c.GroupKey, c.Username); err != nil {
		return err
	}

	if err := actions.AddBattery(ctx, payload); err != nil {
		return err
	}

	updatedPayload, _ := json.Marshal(payload)
	hub.Broadcast(internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), InfoType: "BATTERY_ADDED", Payload: updatedPayload})
	return nil
}

func (c *Client) handleRemoveBattery(ctx context.Context, hub *Hub, rawPayload json.RawMessage) error {
	var payload internal.Battery
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return errInvalidPayload(err)
	}

	payload.GroupKey = c.GroupKey
	if _, err := actions.GetMember(ctx, c.GroupKey, c.Username); err != nil {
		return err
	}
	if _, err := actions.GetBattery(ctx, c.GroupKey, payload.Name); err != nil {
		return err
	}

	if err := actions.RemoveBattery(ctx, payload.GroupKey, payload.Name); err != nil {
		return err
	}

	updatedPayload, _ := json.Marshal(payload)
	hub.Broadcast(internal.Message{GroupKey: c.GroupKey, Timestamp: time.Now(), InfoType: "BATTERY_REMOVED", Payload: updatedPayload})
	return nil
}

func (c *Client) handleDataSync(ctx context.Context, hub *Hub, requestID string) error {
	if _, err := actions.GetMember(ctx, c.GroupKey, c.Username); err != nil {
		return err
	}

	members, err := actions.GetAllMembers(ctx, c.GroupKey)
	if err != nil {
		return err
	}
	batteries, err := actions.GetAllBatteries(ctx, c.GroupKey)
	if err != nil {
		return err
	}
	logs, err := actions.GetAllLogs(ctx, c.GroupKey)
	if err != nil {
		return err
	}
	group, err := actions.GetGroup(ctx, c.GroupKey)
	if err != nil {
		return err
	}
	matches, err := actions.GetTeamMatches(ctx, group.TeamNumber, group.EventKey)
	if err != nil {
		return err
	}
	streams, err := actions.GetStreams(group.TeamNumber, group.EventKey)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(map[string]interface{}{
		"members":   members,
		"batteries": batteries,
		"logs":      logs,
		"group":     group,
		"matches":   matches,
		"streams":   streams,
	})
	if err != nil {
		return err
	}
	response, err := json.Marshal(internal.Message{
		GroupKey:  c.GroupKey,
		Timestamp: time.Now(),
		InfoType:  "SYNC",
		Payload:   payload,
		RequestID: requestID,
	})
	if err != nil {
		return err
	}

	hub.SendTo(c, response)
	return nil
}
