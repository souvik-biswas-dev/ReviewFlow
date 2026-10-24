package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func startHub(t *testing.T) *Hub {
	t.Helper()
	h := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)
	t.Cleanup(func() { cancel(); <-h.Done() })
	return h
}

func fake(h *Hub, room, user, name string, buf int) *Client {
	return &Client{hub: h, send: make(chan []byte, buf), room: room, userID: user, username: name}
}

func recv(t *testing.T, c *Client) Message {
	t.Helper()
	select {
	case b, ok := <-c.send:
		if !ok {
			t.Fatalf("send channel closed")
		}
		var m Message
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for frame")
	}
	return Message{}
}

func expectNone(t *testing.T, c *Client) {
	t.Helper()
	select {
	case b := <-c.send:
		t.Fatalf("unexpected frame: %s", b)
	case <-time.After(80 * time.Millisecond):
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func TestPresenceJoinLeaveAndTabDedupe(t *testing.T) {
	h := startHub(t)
	a := fake(h, "r1", "u1", "alice", 16)
	if err := h.Register(a); err != nil {
		t.Fatal(err)
	}
	if m := recv(t, a); m.Type != TypePresenceList {
		t.Fatalf("want presence_list, got %s", m.Type)
	}

	b := fake(h, "r1", "u2", "bob", 16)
	_ = h.Register(b)
	list := recv(t, b)
	if list.Type != TypePresenceList || len(list.Payload.([]any)) != 2 {
		t.Fatalf("bob should see 2 viewers: %+v", list)
	}
	if m := recv(t, a); m.Type != TypePresenceJoin || m.SenderUsername != "bob" {
		t.Fatalf("alice should see bob join: %+v", m)
	}

	// A second tab for bob must not announce another join.
	b2 := fake(h, "r1", "u2", "bob", 16)
	_ = h.Register(b2)
	recv(t, b2) // presence_list
	expectNone(t, a)
	if got := len(h.Presence("r1")); got != 2 {
		t.Fatalf("tabs should collapse: %d viewers", got)
	}

	// Closing one tab is silent; closing the last announces the leave.
	h.Unregister(b2)
	expectNone(t, a)
	h.Unregister(b)
	if m := recv(t, a); m.Type != TypePresenceLeave || m.SenderUsername != "bob" {
		t.Fatalf("want leave, got %+v", m)
	}
	waitFor(t, func() bool { return len(h.Presence("r1")) == 1 })
}

func TestBroadcastIsRoomScoped(t *testing.T) {
	h := startHub(t)
	a := fake(h, "r1", "u1", "alice", 16)
	b := fake(h, "r2", "u2", "bob", 16)
	_ = h.Register(a)
	_ = h.Register(b)
	recv(t, a)
	recv(t, b)

	h.BroadcastToRoom("r1", NewMessage(TypeReviewAdded, "", map[string]any{"id": "x"}))
	m := recv(t, a)
	if m.Type != TypeReviewAdded || m.SnippetID != "r1" {
		t.Fatalf("bad frame: %+v", m)
	}
	expectNone(t, b)
}

func TestSlowClientIsEvictedAndOthersUnaffected(t *testing.T) {
	h := startHub(t)
	fast := fake(h, "r1", "u1", "fast", 64)
	slow := fake(h, "r1", "u2", "slow", 2) // never drained
	_ = h.Register(fast)
	_ = h.Register(slow)
	recv(t, fast)
	recv(t, fast) // fast sees slow's join

	for i := 0; i < 10; i++ {
		h.BroadcastToRoom("r1", NewMessage(TypeReviewAdded, "", i))
	}
	waitFor(t, func() bool { return len(h.Presence("r1")) == 1 })

	// fast received every broadcast, then the leave for the evicted client.
	got := 0
	var sawLeave bool
	for i := 0; i < 11; i++ {
		m := recv(t, fast)
		if m.Type == TypeReviewAdded {
			got++
		}
		if m.Type == TypePresenceLeave && m.SenderUsername == "slow" {
			sawLeave = true
		}
	}
	if got != 10 || !sawLeave {
		t.Fatalf("fast client got %d broadcasts, leave=%v", got, sawLeave)
	}
	// Unregistering the already-evicted client must be a harmless no-op.
	h.Unregister(slow)
}

func TestRelayExcludesSender(t *testing.T) {
	h := startHub(t)
	a := fake(h, "r1", "u1", "alice", 16)
	b := fake(h, "r1", "u2", "bob", 16)
	_ = h.Register(a)
	_ = h.Register(b)
	recv(t, a)
	recv(t, a) // join
	recv(t, b)

	h.relay(a, NewMessage(TypeTyping, "r1", nil).bytes())
	if m := recv(t, b); m.Type != TypeTyping {
		t.Fatalf("bob should get typing, got %s", m.Type)
	}
	expectNone(t, a)
}

func TestShutdownClosesClientsAndRejectsRegister(t *testing.T) {
	h := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)
	a := fake(h, "r1", "u1", "alice", 16)
	_ = h.Register(a)
	recv(t, a)
	cancel()
	<-h.Done()
	if _, ok := <-a.send; ok {
		t.Fatal("send should be closed after shutdown")
	}
	if err := h.Register(fake(h, "r1", "u2", "b", 1)); err != ErrHubStopped {
		t.Fatalf("want ErrHubStopped, got %v", err)
	}
}

// Hammer register/unregister/broadcast/presence concurrently; -race must stay quiet.
func TestConcurrentUse(t *testing.T) {
	h := startHub(t)
	done := make(chan struct{})
	for g := 0; g < 8; g++ {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 50; i++ {
				c := fake(h, "room", string(rune('a'+g)), "u", 4)
				_ = h.Register(c)
				h.BroadcastToRoom("room", NewMessage(TypeReviewAdded, "", i))
				_ = h.Presence("room")
				h.Stats()
				h.Unregister(c)
			}
		}(g)
	}
	for g := 0; g < 8; g++ {
		<-done
	}
	waitFor(t, func() bool { r, c := h.Stats(); return r == 0 && c == 0 })
}
