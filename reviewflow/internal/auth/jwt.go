package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// tokenTTL is the session lifetime. Spec: tokens expire after 7 days.
const tokenTTL = 7 * 24 * time.Hour

// Claims is the JWT payload. The custom fields (userId, githubUsername) sit
// alongside RegisteredClaims, which supplies the standard `exp`/`iat` dates —
// so the encoded payload is exactly { userId, githubUsername, exp, iat }.
type Claims struct {
	UserID         string `json:"userId"`
	GitHubUsername string `json:"githubUsername"`
	jwt.RegisteredClaims
}

// GenerateToken signs a 7-day HS256 JWT for the given user.
func GenerateToken(secret, userID, githubUsername string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:         userID,
		GitHubUsername: githubUsername,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken validates the signature and expiry and returns the claims.
func ParseToken(secret, tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		// Pin the algorithm to HMAC: reject anything else (e.g. "none" or an
		// RS256 token) to defend against algorithm-confusion attacks where an
		// attacker swaps the alg header to bypass signature verification.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
