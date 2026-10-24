package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"reviewflow/internal/auth"
	"reviewflow/internal/config"
	"reviewflow/internal/store"
)

const (
	notificationsPageSize = 20
	notificationsMaxPage  = 50
)

// registerNotifications wires the bell-icon endpoints. Rows are written by the
// addReview mutation; these only read and mark them.
func registerNotifications(r *gin.Engine, cfg *config.Config, st *store.Store) {
	g := r.Group("/notifications", auth.Required(cfg.JWTSecret))

	// GET /notifications?page=N: newest 20 plus the unread count for the badge.
	g.GET("", func(c *gin.Context) {
		page, _ := strconv.Atoi(c.Query("page"))
		if page < 0 {
			page = 0
		}
		if page > notificationsMaxPage {
			page = notificationsMaxPage
		}
		uid := auth.UserID(c)
		rows, err := st.ListNotifications(c.Request.Context(), uid, page, notificationsPageSize)
		if err != nil {
			slog.Error("notifications: list", "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		unread, err := st.UnreadCount(c.Request.Context(), uid)
		if err != nil {
			unread = 0 // non-fatal: the badge just shows zero
		}
		c.JSON(http.StatusOK, gin.H{"notifications": rows, "unreadCount": unread, "page": page})
	})

	// POST /notifications/read: called when the dropdown opens.
	g.POST("/read", func(c *gin.Context) {
		if err := st.MarkAllRead(c.Request.Context(), auth.UserID(c)); err != nil {
			slog.Error("notifications: mark read", "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
}
