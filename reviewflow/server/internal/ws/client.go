package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// SendBuffer is the bounded per-client outbound queue. A client that lets
	// it fill up is evicted instead of stalling the room.
	SendBuffer = 256

	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = pongWait * 9 / 10
	maxMessageSize = 64 * 1024
	typingInterval = 400 * time.Millisecond
)

// Client is one WebSocket connection. Identity fields are immutable; all
// coordination with other clients goes through the hub.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	room     string
	userID   string
	username string

	closeOnce  sync.Once
	lastTyping time.Time // touched only by readPump
}

func NewClient(hub *Hub, conn *websocket.Conn, room, userID, username string) *Client {
	return &Client{
		hub: hub, conn: conn, send: make(chan []byte, SendBuffer),
		room: room, userID: userID, username: username,
	}
}

// Start launches the read and write pumps. Call after Hub.Register succeeds.
func (c *Client) Start() {
	go c.writePump()
	go c.readPump()
}

func (c *Client) closeConn() {
	c.closeOnce.Do(func() {
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.closeConn()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		mt, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				slog.Debug("ws: read", "user", c.userID, "room", c.room, "err", err)
			}
			return
		}
		// Any inbound frame proves the peer is alive.
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		if mt != websocket.TextMessage {
			continue
		}
		c.handleInbound(raw)
	}
}

// handleInbound acts only on the frame types a client may originate; the
// server-authoritative ones (review_added, presence_*, ai_*) are ignored.
func (c *Client) handleInbound(raw []byte) {
	var in inbound
	if err := json.Unmarshal(raw, &in); err != nil {
		return
	}
	switch in.Type {
	case TypePing:
		// Browser JS cannot send WS ping frames, so the SPA sends app-level pings.
		c.hub.sendTo(c, NewMessage(TypePong, c.room, nil).bytes())
	case TypeTyping:
		if time.Since(c.lastTyping) < typingInterval {
			return
		}
		c.lastTyping = time.Now()
		var payload any
		if len(in.Payload) > 0 {
			_ = json.Unmarshal(in.Payload, &payload)
		}
		// Re-stamp the sender from the authenticated identity: no impersonation.
		m := NewMessage(TypeTyping, c.room, payload)
		m.SenderID, m.SenderUsername = c.userID, c.username
		c.hub.relay(c, m.bytes())
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.closeConn()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok { // hub closed us
				_ = c.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
