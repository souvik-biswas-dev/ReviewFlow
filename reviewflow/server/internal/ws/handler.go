package ws

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// HandlerConfig wires the socket endpoint to the rest of the app without
// importing it (keeps this package testable on its own).
type HandlerConfig struct {
	Hub          *Hub
	Authenticate func(r *http.Request) (userID, username string, ok bool)
	RoomExists   func(ctx context.Context, room string) bool
	CheckOrigin  func(r *http.Request) bool
}

// Handler serves GET /ws/:snippetID.
//
// Auth and the room check happen before the upgrade, because once upgraded we
// can no longer answer with a normal HTTP error.
func Handler(cfg HandlerConfig) gin.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     cfg.CheckOrigin,
	}
	return func(c *gin.Context) {
		room := c.Param("snippetID")
		userID, username, ok := cfg.Authenticate(c.Request)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if !cfg.RoomExists(c.Request.Context(), room) {
			c.JSON(http.StatusNotFound, gin.H{"error": "snippet not found"})
			return
		}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return // Upgrade already replied (e.g. 403 for a bad origin)
		}
		client := NewClient(cfg.Hub, conn, room, userID, username)
		if err := cfg.Hub.Register(client); err != nil {
			_ = conn.Close()
			return
		}
		client.Start()
	}
}

// OriginChecker allows the configured browser origins, same-host requests (the
// Caddy / single-binary deployment) and non-browser clients (no Origin header;
// those still need a valid JWT).
func OriginChecker(allowed []string) func(*http.Request) bool {
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		set[strings.TrimRight(o, "/")] = true
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		if set[strings.TrimRight(origin, "/")] {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host)
	}
}
