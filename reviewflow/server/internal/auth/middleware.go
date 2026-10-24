package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	ctxUserID   = "rf.userID"
	ctxUsername = "rf.username"
)

type ctxKey struct{}

// Identity is the authenticated caller.
type Identity struct {
	UserID   string
	Username string
}

// ExtractToken reads the session token from, in order:
//  1. Authorization: Bearer <tok>  (fetch / GraphQL)
//  2. ?token=<tok>                  (browsers cannot set headers on a WebSocket)
//  3. the HttpOnly cookie           (same-origin fallback)
func ExtractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if q := r.URL.Query().Get("token"); q != "" {
		return q
	}
	if c, err := r.Cookie(CookieName); err == nil {
		return c.Value
	}
	return ""
}

// Authenticate validates the request's token, returning the caller or false.
func Authenticate(secret string, r *http.Request) (Identity, bool) {
	claims, err := ParseToken(secret, ExtractToken(r))
	if err != nil {
		return Identity{}, false
	}
	return Identity{UserID: claims.UserID, Username: claims.GithubUsername}, true
}

// Required rejects the request with 401 unless a valid token is present.
func Required(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ExtractToken(c.Request) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authentication token"})
			return
		}
		id, ok := Authenticate(secret, c.Request)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}
		c.Set(ctxUserID, id.UserID)
		c.Set(ctxUsername, id.Username)
		c.Next()
	}
}

// Soft attaches the caller's identity (to both the gin context and the request
// context) when a valid token is present, but never rejects. GraphQL resolvers
// decide which operations need a login.
func Soft(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if id, ok := Authenticate(secret, c.Request); ok {
			c.Set(ctxUserID, id.UserID)
			c.Set(ctxUsername, id.Username)
			c.Request = c.Request.WithContext(WithIdentity(c.Request.Context(), id))
		}
		c.Next()
	}
}

// UserID returns the authenticated user id set by Required/Soft.
func UserID(c *gin.Context) string { return c.GetString(ctxUserID) }

// WithIdentity stores the caller on a context (used by GraphQL resolvers).
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the caller stored by WithIdentity.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok && id.UserID != ""
}
