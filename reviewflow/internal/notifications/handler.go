// Package notifications exposes the bell-icon REST endpoints used by the
// dashboard. Notification rows are written by the addReview resolver; this
// package only reads/marks them.
package notifications

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"reviewflow/internal/auth"
	"reviewflow/internal/db"
)

const (
	defaultLimit = int64(20)
	maxPage      = 50
)

// Handler holds the deps each endpoint needs.
type Handler struct {
	db *db.Client
}

func NewHandler(database *db.Client) *Handler {
	return &Handler{db: database}
}

// notificationItem is the response shape: the raw notification fields plus a
// joined snippet title so the bell can show "X reviewed your snippet «...»".
type notificationItem struct {
	ID           string    `json:"id"`
	SnippetID    string    `json:"snippetId"`
	SnippetTitle string    `json:"snippetTitle"`
	ReviewID     string    `json:"reviewId"`
	Read         bool      `json:"read"`
	CreatedAt    time.Time `json:"createdAt"`
}

// List handles GET /notifications?page=N. Returns the newest 20 notifications
// for the caller and an unreadCount for the bell badge.
func (h *Handler) List(c *gin.Context) {
	userID, err := callerOID(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	page := parsePage(c.Query("page"))
	ctx := c.Request.Context()

	cur, err := h.db.Database.Collection(db.NotificationsCollection).Find(
		ctx,
		bson.M{"userId": userID},
		options.Find().
			SetSort(bson.D{{Key: "createdAt", Value: -1}}).
			SetSkip(page*defaultLimit).
			SetLimit(defaultLimit),
	)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to load notifications"})
		return
	}
	defer cur.Close(ctx)

	var rows []db.Notification
	if err := cur.All(ctx, &rows); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to decode notifications"})
		return
	}

	// Batch-load the referenced snippet titles in one round-trip.
	titles, err := h.loadSnippetTitles(ctx, rows)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to enrich notifications"})
		return
	}

	items := make([]notificationItem, 0, len(rows))
	for _, n := range rows {
		items = append(items, notificationItem{
			ID:           n.ID.Hex(),
			SnippetID:    n.SnippetID.Hex(),
			SnippetTitle: titles[n.SnippetID],
			ReviewID:     n.ReviewID.Hex(),
			Read:         n.Read,
			CreatedAt:    n.CreatedAt,
		})
	}

	unread, err := h.db.Database.Collection(db.NotificationsCollection).
		CountDocuments(ctx, bson.M{"userId": userID, "read": false})
	if err != nil {
		unread = 0 // non-fatal for the bell
	}

	c.JSON(http.StatusOK, gin.H{
		"notifications": items,
		"unreadCount":   unread,
		"page":          page,
	})
}

// MarkAllRead handles POST /notifications/read. Called when the bell dropdown
// opens; flips every unread notification for the caller to read.
func (h *Handler) MarkAllRead(c *gin.Context) {
	userID, err := callerOID(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	_, err = h.db.Database.Collection(db.NotificationsCollection).UpdateMany(
		c.Request.Context(),
		bson.M{"userId": userID, "read": false},
		bson.M{"$set": bson.M{"read": true}},
	)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to mark read"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- helpers ---

func callerOID(c *gin.Context) (primitive.ObjectID, error) {
	userID := c.GetString(auth.ContextUserIDKey)
	if userID == "" {
		return primitive.NilObjectID, errors.New("missing user id")
	}
	return primitive.ObjectIDFromHex(userID)
}

func parsePage(raw string) int64 {
	if raw == "" {
		return 0
	}
	p, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || p < 0 {
		return 0
	}
	if p > maxPage {
		return maxPage
	}
	return p
}

func (h *Handler) loadSnippetTitles(ctx context.Context, rows []db.Notification) (map[primitive.ObjectID]string, error) {
	out := map[primitive.ObjectID]string{}
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]primitive.ObjectID, 0, len(rows))
	seen := map[primitive.ObjectID]struct{}{}
	for _, r := range rows {
		if _, ok := seen[r.SnippetID]; ok {
			continue
		}
		seen[r.SnippetID] = struct{}{}
		ids = append(ids, r.SnippetID)
	}
	cur, err := h.db.Database.Collection(db.SnippetsCollection).Find(
		ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		options.Find().SetProjection(bson.M{"_id": 1, "title": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	type titleRow struct {
		ID    primitive.ObjectID `bson:"_id"`
		Title string             `bson:"title"`
	}
	var titleRows []titleRow
	if err := cur.All(ctx, &titleRows); err != nil {
		return nil, err
	}
	for _, t := range titleRows {
		out[t.ID] = t.Title
	}
	return out, nil
}
