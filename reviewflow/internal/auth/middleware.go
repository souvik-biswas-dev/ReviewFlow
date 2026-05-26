package auth

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CookieName is the HttpOnly cookie that carries the session JWT.
const CookieName = "rf_token"

// Keys used to expose the authenticated identity to downstream Gin handlers.
const (
	ContextUserIDKey   = "userId"
	ContextUsernameKey = "githubUsername"
)

// contextKey is an unexported type for context.WithValue keys, so it can never
// collide with keys set by other packages.
type contextKey string

const userIDContextKey contextKey = "userId"

// WithUserID returns a copy of ctx carrying the authenticated user id. This is
// how identity reaches the GraphQL layer, which only sees a context.Context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}

// UserIDFromContext extracts the user id injected by WithUserID. ok is false
// when the request is unauthenticated.
func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userIDContextKey).(string)
	return v, ok && v != ""
}

// AuthMiddleware validates the session JWT cookie and, on success, stores the
// caller's identity in both the Gin context and the request's context.Context.
// Any failure aborts the request with a 401 JSON error.
//
// It's written as a factory (returns the handler) rather than a bare
// func(*gin.Context) so the JWT secret can be injected from config instead of
// living in a package global.
func AuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, err := c.Cookie(CookieName)
		if err != nil {
			unauthorized(c, "missing authentication cookie")
			return
		}

		claims, err := ParseToken(secret, tokenString)
		if err != nil {
			unauthorized(c, "invalid or expired token")
			return
		}

		c.Set(ContextUserIDKey, claims.UserID)
		c.Set(ContextUsernameKey, claims.GitHubUsername)
		// Also thread the id through the std context so non-Gin layers (GraphQL
		// resolvers) can read it.
		c.Request = c.Request.WithContext(WithUserID(c.Request.Context(), claims.UserID))
		c.Next()
	}
}

// GraphQLContext is a *soft* counterpart to AuthMiddleware for the /graphql
// endpoint: if a valid session cookie is present it injects the user id into
// the request context, but it never aborts. This lets public queries run
// anonymously while resolvers that require auth check UserIDFromContext.
func GraphQLContext(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if tokenString, err := c.Cookie(CookieName); err == nil {
			if claims, err := ParseToken(secret, tokenString); err == nil {
				c.Request = c.Request.WithContext(WithUserID(c.Request.Context(), claims.UserID))
			}
		}
		c.Next()
	}
}

func unauthorized(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": msg})
}
