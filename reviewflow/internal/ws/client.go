package ws

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

// Timing / sizing constants for a single connection.
const (
	// writeWait is the max time allowed to write one frame to the peer.
	writeWait = 10 * time.Second
	// pongWait is the keepalive timeout: if no readable frame (data or pong)
	// arrives within this window, the read deadline fires and the client is
	// dropped. (Spec: 60s.)
	pongWait = 60 * time.Second
	// pingPeriod must be < pongWait so a ping/pong round-trip can complete
	// before the deadline. 90% of pongWait is the usual choice.
	pingPeriod = (pongWait * 9) / 10
	// maxMessageSize caps inbound frames. Clients only ever send small control
	// messages (typing / ping); large code arrives via GraphQL, not the socket.
	maxMessageSize = 64 * 1024
	// sendBufferSize is the per-client outbound buffer. A client that can't keep
	// up and fills this is evicted as a slow consumer.
	sendBufferSize = 256
)

// Client is one WebSocket connection. Its fields are immutable after creation
// except for the channels it communicates over; all synchronization with other
// clients goes through the hub.
type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan []byte // buffered (sendBufferSize); written by the hub
	snippetId string
	userId    string
	username  string

	// done is closed by the hub (exactly once, in removeClient) to tell
	// writePump to stop. We never close `send` because it has multiple senders
	// (the hub's fan-out and readPump's pong reply) and closing a multi-sender
	// channel would risk a send-on-closed-channel panic.
	done chan struct{}
}

// readPump runs in its own goroutine: it reads frames, maintains the read
// deadline, and routes inbound app messages. It owns the connection's single
// reader. On any read error it unregisters the client from the hub.
func (c *Client) readPump() {
	defer func() {
		// Idempotent: if the hub already evicted us (slow drop), this is a no-op.
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	// A pong control frame (auto-sent by browsers in response to our ping)
	// resets the deadline — this is the primary keepalive.
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseNormalClosure,    // 1000 – clean client disconnect
				websocket.CloseGoingAway,        // 1001 – browser navigating away
				websocket.CloseNoStatusReceived, // 1005 – Render proxy spin-down (no status frame)
				websocket.CloseAbnormalClosure,  // 1006 – network drop
			) {
				log.Printf("ws: read error (user=%s snippet=%s): %v", c.userId, c.snippetId, err)
			}
			return
		}
		// Any inbound frame also counts as liveness.
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		c.handleInbound(raw)
	}
}

// writePump runs in its own goroutine and owns the connection's single writer.
// It drains the send buffer, emits periodic WS ping frames, and shuts down when
// the hub closes done.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// send is never closed in normal operation; guard anyway.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return // peer gone; readPump will observe it and unregister
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.done:
			// Hub asked us to stop (normal unregister or slow-client eviction).
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		}
	}
}

// handleInbound parses a client frame and acts on the message types a client is
// allowed to originate. Server-authoritative types (review_added, presence_*,
// ai_review_ready) are ignored when sent by a client.
func (c *Client) handleInbound(raw []byte) {
	var in Message
	if err := json.Unmarshal(raw, &in); err != nil {
		return // ignore malformed frames
	}

	switch in.Type {
	case MessagePing:
		// App-level heartbeat. Browser JS can't send WS ping frames, so this is
		// how a web client proves liveness; we reply with a pong straight to its
		// own buffer (non-blocking — if even that is full, the hub will evict).
		if pong, err := NewMessage(MessagePong, c.snippetId, nil); err == nil {
			if b, err := json.Marshal(pong); err == nil {
				trySend(c, b)
			}
		}

	case MessageTyping:
		// Relay "typing" to the rest of the room. We re-stamp the sender from
		// the authenticated identity so a client can't impersonate anyone.
		out := Message{
			Type:           MessageTyping,
			SnippetID:      c.snippetId,
			Payload:        in.Payload,
			SenderID:       c.userId,
			SenderUsername: c.username,
			Timestamp:      time.Now().UTC(),
		}
		if b, err := json.Marshal(out); err == nil {
			c.hub.broadcast <- &broadcastMessage{snippetId: c.snippetId, data: b, exclude: c}
		}

	default:
		// Ignore everything else a client might try to send.
	}
}
