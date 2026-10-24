// Package ws is ReviewFlow's real-time engine: one room per snippet, a hub that
// owns all room state, and clients that talk to it only through channels.
package ws

import (
	"encoding/json"
	"time"
)

// MessageType identifies the kind of frame on the socket.
type MessageType string

const (
	TypeReviewAdded     MessageType = "review_added"      // server -> room
	TypePresenceJoin    MessageType = "presence_join"     // server -> room
	TypePresenceLeave   MessageType = "presence_leave"    // server -> room
	TypePresenceList    MessageType = "presence_list"     // server -> newcomer
	TypeAIReviewPending MessageType = "ai_review_pending" // server -> room
	TypeAIReviewReady   MessageType = "ai_review_ready"   // server -> room
	TypeAIReviewFailed  MessageType = "ai_review_failed"  // server -> room
	TypeTyping          MessageType = "typing"            // client -> room
	TypePing            MessageType = "ping"              // client -> server
	TypePong            MessageType = "pong"              // server -> client
)

// Message is the JSON envelope for every frame. Payload is deliberately
// untyped: the hub shuffles frames without knowing each payload's shape.
type Message struct {
	Type           MessageType `json:"type"`
	SnippetID      string      `json:"snippetId"`
	Payload        any         `json:"payload"`
	SenderID       string      `json:"senderId,omitempty"`
	SenderUsername string      `json:"senderUsername,omitempty"`
	Timestamp      time.Time   `json:"timestamp"`
}

// Presence identifies one viewer (multiple tabs collapse to one entry).
type Presence struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
}

// AIFailure is the payload of ai_review_failed.
type AIFailure struct {
	SnippetID string `json:"snippetId"`
	Error     string `json:"error"`
}

// AIPending is the payload of ai_review_pending.
type AIPending struct {
	SnippetID string `json:"snippetId"`
}

// NewMessage stamps the current time.
func NewMessage(t MessageType, snippetID string, payload any) Message {
	return Message{Type: t, SnippetID: snippetID, Payload: payload, Timestamp: time.Now().UTC()}
}

func (m Message) bytes() []byte {
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}

// inbound is what clients are allowed to send.
type inbound struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
