package ws

import (
	"encoding/json"
	"time"
)

// MessageType enumerates the kinds of envelopes that flow over the socket.
type MessageType string

const (
	MessageReviewAdded   MessageType = "review_added"    // a new review was posted
	MessagePresenceJoin  MessageType = "presence_join"   // a user joined the room
	MessagePresenceLeave MessageType = "presence_leave"  // a user left the room
	MessagePresenceList  MessageType = "presence_list"   // full current viewer list
	MessageAIReviewReady MessageType = "ai_review_ready" // Gemini analysis complete
	MessageTyping        MessageType = "typing"          // a user is typing a review
	MessagePing          MessageType = "ping"            // app-level keepalive (client -> server)
	MessagePong          MessageType = "pong"            // app-level keepalive (server -> client)
)

// Message is the JSON envelope exchanged with every client.
//
// Payload is kept as raw JSON so the envelope is type-agnostic: the hub shuffles
// bytes between rooms without needing to understand each payload's concrete
// shape. Producers build the payload (a Presence, a review object, ...) and the
// receiving client decodes it based on Type.
type Message struct {
	Type           MessageType     `json:"type"`
	SnippetID      string          `json:"snippetId"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	SenderID       string          `json:"senderId,omitempty"`
	SenderUsername string          `json:"senderUsername,omitempty"`
	Timestamp      time.Time       `json:"timestamp"`
}

// Presence identifies a viewer. It is the payload for presence_join /
// presence_leave (a single Presence) and presence_list (a []Presence).
type Presence struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
}

// NewMessage builds an envelope, marshaling payload (which may be nil) into the
// raw Payload field and stamping the current time.
func NewMessage(t MessageType, snippetID string, payload any) (Message, error) {
	msg := Message{
		Type:      t,
		SnippetID: snippetID,
		Timestamp: time.Now().UTC(),
	}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return Message{}, err
		}
		msg.Payload = raw
	}
	return msg, nil
}
