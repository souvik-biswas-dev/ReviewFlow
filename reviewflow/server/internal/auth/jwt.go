// Package auth implements session tokens, the HTTP middleware that reads them,
// and the login endpoints (GitHub OAuth, guest login, /auth/me, logout).
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// TokenTTL is the session lifetime.
	TokenTTL = 7 * 24 * time.Hour
	// CookieName is the HttpOnly cookie that carries the session JWT.
	CookieName = "rf_token"
)

// Claims is the JWT payload: { userId, githubUsername, exp, iat }.
type Claims struct {
	UserID         string `json:"userId"`
	GithubUsername string `json:"githubUsername"`
	jwt.RegisteredClaims
}

// GenerateToken signs a 7-day HS256 token.
func GenerateToken(secret, userID, username string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:         userID,
		GithubUsername: username,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ParseToken validates signature and expiry. The algorithm is pinned to HS256,
// which rejects "none" and RS256 algorithm-confusion attacks.
func ParseToken(secret, token string) (*Claims, error) {
	if token == "" {
		return nil, errors.New("empty token")
	}
	claims := &Claims{}
	t, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	if !t.Valid || claims.UserID == "" {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
