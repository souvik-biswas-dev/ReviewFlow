package ws

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"reviewflow/internal/auth"
)

// Handler upgrades HTTP requests to WebSocket connections and attaches them to
// the hub. One Handler is shared by all connections.
type Handler struct {
	hub      *Hub
	upgrader websocket.Upgrader
}

// NewHandler builds the WS route handler. allowedOrigin should be the browser
// origin permitted to open sockets (the SvelteKit app, e.g. http://localhost:5173).
func NewHandler(hub *Hub, allowedOrigin string) *Handler {
	return &Handler{
		hub: hub,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			// Reject cross-origin upgrades. The browser sends Origin; only our
			// own frontend may connect. (Auth is also enforced before upgrade.)
			CheckOrigin: func(r *http.Request) bool {
				return r.Header.Get("Origin") == allowedOrigin
			},
		},
	}
}

// ServeWS handles GET /ws/:snippetId.
//
// AuthMiddleware must run before this handler so the JWT cookie is validated and
// the user identity is present in the Gin context *before* the upgrade — once a
// connection is upgraded we can no longer write a normal HTTP error.
func (h *Handler) ServeWS(c *gin.Context) {
	snippetID := c.Param("snippetId")
	if snippetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing snippetId"})
		return
	}

	userID := c.GetString(auth.ContextUserIDKey)
	username := c.GetString(auth.ContextUsernameKey)
	if userID == "" {
		// Should be unreachable behind AuthMiddleware, but fail closed.
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade writes its own HTTP error response on failure.
		log.Printf("ws: upgrade failed (user=%s snippet=%s): %v", userID, snippetID, err)
		return
	}

	client := &Client{
		hub:       h.hub,
		conn:      conn,
		send:      make(chan []byte, sendBufferSize),
		snippetId: snippetID,
		userId:    userID,
		username:  username,
		done:      make(chan struct{}),
	}

	// Register before starting the pumps so the join broadcast / presence_list
	// are produced against a consistent room membership. The presence_list is
	// buffered in `send` until writePump starts draining it.
	h.hub.register <- client

	go client.writePump()
	go client.readPump()
}
