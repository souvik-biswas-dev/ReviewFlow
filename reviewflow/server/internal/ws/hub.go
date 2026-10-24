package ws

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
)

// ErrHubStopped is returned when registering after the hub has shut down.
var ErrHubStopped = errors.New("ws: hub stopped")

// envelope is one fan-out request.
type envelope struct {
	room   string
	data   []byte
	except *Client // skip this sender (typing relay, join announcements)
	only   *Client // deliver to just this client (pong, presence list)
}

// Hub owns every room.
//
// Concurrency contract
//   - rooms is mutated ONLY inside the Run goroutine, which is driven by the
//     register, unregister and broadcast channels. Nothing else writes it, so
//     there is no write/write race by construction.
//   - Writes take mu.Lock and external readers (Presence, Stats) take mu.RLock,
//     so observers on other goroutines (HTTP handlers, tests) see consistent
//     snapshots. Run itself is the sole writer, so it reads without locking.
//   - Only Run sends to, and closes, a client's send channel. That removes the
//     classic "send on closed channel" panic: readers never touch send.
//   - Fan-out never blocks: a client whose 256-slot buffer is full is evicted
//     rather than allowed to stall the room.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[*Client]struct{}

	register   chan *Client
	unregister chan *Client
	broadcast  chan envelope

	done chan struct{} // closed when Run returns
}

func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[string]map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan envelope),
		done:       make(chan struct{}),
	}
}

// Run is the hub's single owner goroutine. Call it exactly once.
func (h *Hub) Run(ctx context.Context) {
	defer close(h.done)
	for {
		select {
		case <-ctx.Done():
			h.shutdown()
			return
		case c := <-h.register:
			h.handleRegister(c)
		case c := <-h.unregister:
			h.drop(c)
		case e := <-h.broadcast:
			h.handleBroadcast(e)
		}
	}
}

// Done is closed once Run has returned.
func (h *Hub) Done() <-chan struct{} { return h.done }

// Register adds a client. It blocks until the hub goroutine accepts it.
func (h *Hub) Register(c *Client) error {
	select {
	case h.register <- c:
		return nil
	case <-h.done:
		return ErrHubStopped
	}
}

// Unregister removes a client; idempotent, and a no-op once the hub is stopped.
func (h *Hub) Unregister(c *Client) {
	select {
	case h.unregister <- c:
	case <-h.done:
	}
}

// BroadcastToRoom pushes a typed frame to everyone viewing a snippet. This is
// what GraphQL resolvers and the AI pipeline call.
func (h *Hub) BroadcastToRoom(room string, msg Message) {
	msg.SnippetID = room
	data := msg.bytes()
	if data == nil {
		slog.Error("ws: marshal broadcast", "room", room, "type", msg.Type)
		return
	}
	h.send(envelope{room: room, data: data})
}

func (h *Hub) relay(from *Client, data []byte) {
	h.send(envelope{room: from.room, data: data, except: from})
}

func (h *Hub) sendTo(c *Client, data []byte) {
	h.send(envelope{room: c.room, data: data, only: c})
}

func (h *Hub) send(e envelope) {
	select {
	case h.broadcast <- e:
	case <-h.done:
	}
}

// Presence returns the distinct users in a room, sorted by name.
func (h *Hub) Presence(room string) []Presence {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return presenceOf(h.rooms[room])
}

// Stats reports live rooms and connections.
func (h *Hub) Stats() (rooms, conns int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, r := range h.rooms {
		conns += len(r)
	}
	return len(h.rooms), conns
}

// ---------------------------------------------------------------------------
// Everything below runs only on the Run goroutine.
// ---------------------------------------------------------------------------

func (h *Hub) handleRegister(c *Client) {
	h.mu.Lock()
	room := h.rooms[c.room]
	if room == nil {
		room = make(map[*Client]struct{})
		h.rooms[c.room] = room
	}
	room[c] = struct{}{}
	firstForUser := countUser(room, c.userID) == 1
	h.mu.Unlock()

	var slow []*Client
	// Seed the newcomer with who is already here (itself included).
	list := NewMessage(TypePresenceList, c.room, presenceOf(room))
	slow = append(slow, h.deliver(room, list.bytes(), nil, c)...)

	// Announce the join only for the user's first connection (tabs collapse).
	if firstForUser {
		join := presenceFrame(TypePresenceJoin, c)
		slow = append(slow, h.deliver(room, join, c, nil)...)
	}
	for _, s := range slow {
		h.drop(s)
	}
}

func (h *Hub) handleBroadcast(e envelope) {
	room := h.rooms[e.room]
	if room == nil {
		return
	}
	for _, s := range h.deliver(room, e.data, e.except, e.only) {
		h.drop(s)
	}
}

// deliver does a non-blocking send to each target and returns clients whose
// buffers were full. The caller drops them after iteration.
func (h *Hub) deliver(room map[*Client]struct{}, data []byte, except, only *Client) (slow []*Client) {
	if data == nil {
		return nil
	}
	if only != nil {
		if _, ok := room[only]; ok && !trySend(only, data) {
			slow = append(slow, only)
		}
		return slow
	}
	for c := range room {
		if c == except {
			continue
		}
		if !trySend(c, data) {
			slow = append(slow, c)
		}
	}
	return slow
}

func trySend(c *Client, data []byte) bool {
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}

// drop removes a client, closes its send channel and socket, and announces the
// leave if it was the user's last connection. Announcements can themselves
// expose more slow clients, so this works through a queue instead of recursing.
func (h *Hub) drop(first *Client) {
	queue := []*Client{first}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]

		removed, lastForUser := h.detach(c)
		if !removed {
			continue // already gone: unregister after eviction is a no-op
		}
		c.closeConn()
		if lastForUser {
			if room := h.rooms[c.room]; room != nil {
				queue = append(queue, h.deliver(room, presenceFrame(TypePresenceLeave, c), nil, nil)...)
			}
		}
	}
}

func (h *Hub) detach(c *Client) (removed, lastForUser bool) {
	h.mu.Lock()
	room, ok := h.rooms[c.room]
	if !ok {
		h.mu.Unlock()
		return false, false
	}
	if _, ok := room[c]; !ok {
		h.mu.Unlock()
		return false, false
	}
	delete(room, c)
	lastForUser = countUser(room, c.userID) == 0
	if len(room) == 0 {
		delete(h.rooms, c.room)
	}
	h.mu.Unlock()

	close(c.send) // safe: only this goroutine ever sends on it
	return true, lastForUser
}

func (h *Hub) shutdown() {
	h.mu.Lock()
	var all []*Client
	for _, room := range h.rooms {
		for c := range room {
			all = append(all, c)
		}
	}
	h.rooms = make(map[string]map[*Client]struct{})
	h.mu.Unlock()

	for _, c := range all {
		close(c.send)
		c.closeConn()
	}
}

func countUser(room map[*Client]struct{}, userID string) int {
	n := 0
	for c := range room {
		if c.userID == userID {
			n++
		}
	}
	return n
}

func presenceOf(room map[*Client]struct{}) []Presence {
	seen := map[string]Presence{}
	for c := range room {
		seen[c.userID] = Presence{UserID: c.userID, Username: c.username}
	}
	out := make([]Presence, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Username != out[j].Username {
			return out[i].Username < out[j].Username
		}
		return out[i].UserID < out[j].UserID
	})
	return out
}

func presenceFrame(t MessageType, c *Client) []byte {
	m := NewMessage(t, c.room, Presence{UserID: c.userID, Username: c.username})
	m.SenderID, m.SenderUsername = c.userID, c.username
	return m.bytes()
}
