// Package ws implements ReviewFlow's real-time engine: a room-based WebSocket
// hub where each room corresponds to one snippet id.
//
// Goroutine & channel architecture
// ================================
//
//	          per-client goroutines                         single hub goroutine
//	┌───────────────────────────────────────┐        ┌──────────────────────────────┐
//	│  readPump (1 per client)               │        │          Hub.Run()           │
//	│   • conn.ReadMessage()                 │        │  owns rooms:                 │
//	│   • JSON "ping"  -> "pong"             │        │   map[snippetId]             │
//	│   • "typing"     -> hub.broadcast ─────┼──┐     │       map[*Client]bool       │
//	│   • on exit      -> hub.unregister ────┼┐ │     │                              │
//	│                                        ││ │     │  for { select {              │
//	│  writePump (1 per client)              ││ ├────▶│    <-register                │
//	│   • drains  <-send  (buffered 256) ◀───┼┼─┼─────┤    <-unregister              │
//	│   • ticker  -> WS ping frame           ││ │     │    <-broadcast               │
//	│   • <-done  -> close frame, return ◀───┼┼─┼─────┤  } }                         │
//	└────────────────────────────────────────┘│ │     │                              │
//	         ▲          ▲                       │ │     │  mutations under RWMutex     │
//	         │ send     │ done                  │ │     │  (Run is the only writer);   │
//	         │(buffered)│(closed by hub)        │ │     │  fan-out = non-blocking      │
//	         └──────────┴───────────────────────┘ │     │  send, slow clients dropped  │
//	   register / unregister / broadcast ─────────┘     └──────────────▲───────────────┘
//	          (unbuffered channels)                                     │
//	                                                BroadcastToRoom(snippetId, msg)
//	                                          GraphQL resolvers / Gemini AI service
//
// Concurrency contract:
//   - Hub.Run() is the ONLY goroutine that MUTATES the rooms map.
//   - register / unregister / broadcast carry all write intent into Run().
//   - The RWMutex additionally guards the map so read-only snapshots
//     (RoomPresence, internal fan-out) are safe from any goroutine.
//   - Client code uses channels only — never a mutex.
package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// broadcastMessage is one fan-out request placed on Hub.broadcast: pre-marshaled
// bytes for a room, with an optional client to skip (e.g. don't echo a sender).
type broadcastMessage struct {
	snippetId string
	data      []byte
	exclude   *Client
}

// Hub is the central room registry and message router.
type Hub struct {
	// rooms maps snippetId -> set of connected clients. Guarded by mu; only
	// ever mutated from Run().
	rooms map[string]map[*Client]bool
	mu    sync.RWMutex

	// All unbuffered: the sender blocks until Run() picks the request up, which
	// keeps Run() the single point of serialization.
	register   chan *Client
	unregister chan *Client
	broadcast  chan *broadcastMessage
}

// NewHub allocates a hub. Call Run() in its own goroutine afterwards.
func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[string]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *broadcastMessage),
	}
}

// Run is the hub's event loop. It must run in exactly one goroutine for the
// lifetime of the process; it is the sole mutator of the rooms map.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.handleRegister(c)
		case c := <-h.unregister:
			h.removeAndAnnounce(c)
		case bm := <-h.broadcast:
			h.deliver(bm.snippetId, bm.data, bm.exclude)
		}
	}
}

// BroadcastToRoom is the public entry point used by resolvers / the AI service
// to push a message to everyone viewing a snippet. It marshals the envelope and
// hands it to Run() via the broadcast channel, so fan-out stays single-owner.
func (h *Hub) BroadcastToRoom(snippetID string, msg Message) {
	msg.SnippetID = snippetID
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("ws: marshal broadcast (snippet=%s type=%s): %v", snippetID, msg.Type, err)
		return
	}
	h.broadcast <- &broadcastMessage{snippetId: snippetID, data: data}
}

// RoomPresence returns a de-duplicated snapshot of the users currently viewing a
// snippet. Safe to call from any goroutine (read-only, RLock). Intended for the
// GraphQL layer to seed a subscription's initial state.
func (h *Hub) RoomPresence(snippetID string) []Presence {
	return h.presence(snippetID)
}

// --- Run()-side handlers -----------------------------------------------------

// handleRegister adds a client, announces a join to the room (only when this is
// the user's first connection there), and sends the full viewer list back to
// the newcomer.
func (h *Hub) handleRegister(c *Client) {
	firstForUser := h.addClient(c)

	if firstForUser {
		if b := presenceBytes(MessagePresenceJoin, c); b != nil {
			h.deliver(c.snippetId, b, c) // everyone except the newcomer
		}
	}

	// Seed the newcomer with who's already here.
	if msg, err := NewMessage(MessagePresenceList, c.snippetId, h.presence(c.snippetId)); err == nil {
		if b, err := json.Marshal(msg); err == nil {
			h.sendTo(c, b)
		}
	}
}

// removeAndAnnounce removes a client and, if that was the user's last
// connection in the room, broadcasts a presence_leave. Used for normal
// unregister and for slow-client eviction alike (idempotent per client).
func (h *Hub) removeAndAnnounce(c *Client) {
	removed, lastForUser := h.removeClient(c)
	if removed && lastForUser {
		if b := presenceBytes(MessagePresenceLeave, c); b != nil {
			h.deliver(c.snippetId, b, c)
		}
	}
}

// deliver fans data out to every client in a room (except exclude). A client
// whose 256-slot buffer is full is treated as a slow consumer and evicted.
//
// Slow eviction recurses through removeAndAnnounce -> deliver(leave), but it is
// bounded: each eviction first deletes the client from the map, so no client is
// ever processed twice and recursion depth <= clients in the room.
func (h *Hub) deliver(snippetID string, data []byte, exclude *Client) {
	if data == nil {
		return
	}
	for _, c := range h.snapshotRoom(snippetID) {
		if c == exclude {
			continue
		}
		if !trySend(c, data) {
			h.removeAndAnnounce(c)
		}
	}
}

// sendTo pushes data to a single client, evicting it if its buffer is full.
func (h *Hub) sendTo(c *Client, data []byte) {
	if !trySend(c, data) {
		h.removeAndAnnounce(c)
	}
}

// --- map access (the only places that touch h.rooms) -------------------------

// addClient inserts c into its room (creating the room if needed) and reports
// whether this is the first connection for c.userId there.
func (h *Hub) addClient(c *Client) (firstForUser bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room := h.rooms[c.snippetId]
	if room == nil {
		room = make(map[*Client]bool)
		h.rooms[c.snippetId] = room
	}
	room[c] = true
	return countUser(room, c.userId) == 1
}

// removeClient deletes c from its room, signals its writePump to stop, and
// closes the connection. The membership check guarantees this runs at most once
// per client, so close(c.done) and conn.Close() happen exactly once.
//
// Returns whether the client was present and whether it was the user's last
// connection in the room (so the caller knows to announce a leave).
func (h *Hub) removeClient(c *Client) (removed, lastForUser bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room := h.rooms[c.snippetId]
	if room == nil || !room[c] {
		return false, false
	}
	delete(room, c)
	close(c.done)  // tell writePump to send a close frame and exit
	c.conn.Close() // interrupt readPump's blocked ReadMessage (idempotent close)

	last := countUser(room, c.userId) == 0
	if len(room) == 0 {
		delete(h.rooms, c.snippetId) // clean up empty rooms
	}
	return true, last
}

// snapshotRoom returns the clients in a room as a slice, so fan-out can release
// the read lock before doing any channel sends.
func (h *Hub) snapshotRoom(snippetID string) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()

	room := h.rooms[snippetID]
	out := make([]*Client, 0, len(room))
	for c := range room {
		out = append(out, c)
	}
	return out
}

// presence returns a snapshot of distinct users in a room (one entry per
// userId, so multiple tabs collapse to a single viewer).
func (h *Hub) presence(snippetID string) []Presence {
	h.mu.RLock()
	defer h.mu.RUnlock()

	seen := make(map[string]Presence)
	for c := range h.rooms[snippetID] {
		if _, ok := seen[c.userId]; !ok {
			seen[c.userId] = Presence{UserID: c.userId, Username: c.username}
		}
	}
	out := make([]Presence, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	return out
}

// --- small helpers -----------------------------------------------------------

// trySend performs a non-blocking send to a client's buffer. false => full.
func trySend(c *Client, data []byte) bool {
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}

// countUser counts how many connections in a room belong to userID.
func countUser(room map[*Client]bool, userID string) int {
	n := 0
	for c := range room {
		if c.userId == userID {
			n++
		}
	}
	return n
}

// presenceBytes builds a marshaled presence_join / presence_leave envelope for a
// client, stamped with that client's identity. Returns nil on marshal failure.
func presenceBytes(t MessageType, c *Client) []byte {
	msg, err := NewMessage(t, c.snippetId, Presence{UserID: c.userId, Username: c.username})
	if err != nil {
		return nil
	}
	msg.SenderID = c.userId
	msg.SenderUsername = c.username
	b, err := json.Marshal(msg)
	if err != nil {
		return nil
	}
	return b
}
