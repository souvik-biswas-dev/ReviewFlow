package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"reviewflow/internal/config"
	"reviewflow/internal/db"
)

// GitHub OAuth endpoints and the short-lived cookie used for CSRF protection.
const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubUserURL      = "https://api.github.com/user"
	oauthStateCookie   = "rf_oauth_state"
)

// Handler bundles the dependencies the auth endpoints need. http is a shared
// client with a timeout so a hung GitHub call can't pin a goroutine forever.
type Handler struct {
	cfg  *config.Config
	db   *db.Client
	http *http.Client
}

// NewHandler constructs the auth handler.
func NewHandler(cfg *config.Config, database *db.Client) *Handler {
	return &Handler{
		cfg:  cfg,
		db:   database,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// GitHubLogin (GET /auth/github) kicks off the OAuth flow by redirecting the
// browser to GitHub's authorize page with a random anti-CSRF state value.
func (h *Handler) GitHubLogin(c *gin.Context) {
	state, err := randomState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start oauth"})
		return
	}
	// Remember the state in a short-lived HttpOnly cookie so we can verify, on
	// the callback, that the response corresponds to a request we initiated.
	stateSameSite := http.SameSiteLaxMode
	if h.isProduction() {
		stateSameSite = http.SameSiteNoneMode
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: stateSameSite,
		Secure:   h.isProduction(),
	})

	q := url.Values{}
	q.Set("client_id", h.cfg.GitHubClientID)
	q.Set("redirect_uri", h.cfg.GitHubRedirectURL)
	q.Set("scope", "read:user") // we only need the public profile
	q.Set("state", state)
	c.Redirect(http.StatusTemporaryRedirect, githubAuthorizeURL+"?"+q.Encode())
}

// GitHubCallback (GET /auth/github/callback) completes the flow: verify state,
// exchange the code for a GitHub token, fetch the profile, upsert the user, and
// issue our own session JWT as an HttpOnly cookie before redirecting back to
// the frontend.
func (h *Handler) GitHubCallback(c *gin.Context) {
	// 1. CSRF check: the state echoed by GitHub must match our cookie.
	stateCookie, _ := c.Cookie(oauthStateCookie)
	if stateCookie == "" || c.Query("state") != stateCookie {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid oauth state"})
		return
	}
	c.SetCookie(oauthStateCookie, "", -1, "/", "", false, true) // consume it

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing authorization code"})
		return
	}

	ctx := c.Request.Context()

	// 2. Exchange the code for a GitHub access token.
	ghToken, err := h.exchangeCode(ctx, code)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "github token exchange failed"})
		return
	}

	// 3. Fetch the GitHub profile (id, login, avatar_url).
	profile, err := h.fetchGitHubUser(ctx, ghToken)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch github profile"})
		return
	}

	// 4. Upsert the user (insert on first login, refresh username/avatar after).
	user, err := h.upsertUser(ctx, profile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist user"})
		return
	}

	// 5. Issue our session JWT in an HttpOnly, SameSite=Strict cookie.
	token, err := GenerateToken(h.cfg.JWTSecret, user.ID.Hex(), user.GitHubUsername)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sign token"})
		return
	}
	setSessionCookie(c, token, h.isProduction())

	// 6. Hand the browser back to the SvelteKit app.
	// Also pass the token as a query param so the frontend can store it in
	// localStorage — required when frontend and backend are on different domains
	// (cross-origin cookies are blocked by Chrome's Privacy Sandbox).
	redirectURL := h.cfg.FrontendURL + "/auth/callback?token=" + token
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// Me (GET /auth/me, behind AuthMiddleware) returns the current user.
func (h *Handler) Me(c *gin.Context) {
	oid, err := primitive.ObjectIDFromHex(c.GetString(ContextUserIDKey))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid user id in token"})
		return
	}

	var user db.User
	err = h.db.Database.Collection(db.UsersCollection).
		FindOne(c.Request.Context(), bson.M{"_id": oid}).Decode(&user)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	case err != nil:
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":             user.ID.Hex(),
		"githubUsername": user.GitHubUsername,
		"avatarUrl":      user.AvatarURL,
		"createdAt":      user.CreatedAt,
	})
}

// --- internal helpers ---

// githubProfile is the subset of GitHub's /user response we care about.
type githubProfile struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// exchangeCode swaps the OAuth `code` for a GitHub access token.
func (h *Handler) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", h.cfg.GitHubClientID)
	form.Set("client_secret", h.cfg.GitHubClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", h.cfg.GitHubRedirectURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json") // ask for JSON, not form-encoded

	resp, err := h.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.Error != "" {
		return "", fmt.Errorf("github oauth: %s: %s", body.Error, body.ErrorDesc)
	}
	if body.AccessToken == "" {
		return "", errors.New("github oauth: empty access token")
	}
	return body.AccessToken, nil
}

// fetchGitHubUser reads the authenticated user's public profile.
func (h *Handler) fetchGitHubUser(ctx context.Context, token string) (githubProfile, error) {
	var p githubProfile

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserURL, nil)
	if err != nil {
		return p, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := h.http.Do(req)
	if err != nil {
		return p, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return p, fmt.Errorf("github user: unexpected status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return p, err
	}
	return p, nil
}

// upsertUser inserts the user on first login and otherwise refreshes the
// mutable profile fields, keying on the immutable GitHub numeric id.
func (h *Handler) upsertUser(ctx context.Context, p githubProfile) (*db.User, error) {
	coll := h.db.Database.Collection(db.UsersCollection)
	now := time.Now()

	update := bson.M{
		"$set": bson.M{
			"githubUsername": p.Login,
			"avatarUrl":      p.AvatarURL,
			"updatedAt":      now,
		},
		"$setOnInsert": bson.M{
			"githubId":  p.ID,
			"createdAt": now,
		},
	}
	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After) // return the document *after* the upsert

	var user db.User
	if err := coll.FindOneAndUpdate(ctx, bson.M{"githubId": p.ID}, update, opts).Decode(&user); err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return &user, nil
}

// randomState returns 32 bytes of cryptographically-random hex for OAuth CSRF.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// isProduction reports whether we're running with production-grade defaults
// (currently: should the session cookie be marked Secure).
func (h *Handler) isProduction() bool {
	return h.cfg.Environment == "production"
}

// setSessionCookie writes the JWT as an HttpOnly cookie.
// In production the frontend and backend are on different domains (Cloudflare
// Pages + Render), so we need SameSite=None + Secure for the browser to send
// the cookie on cross-origin requests. In development we use SameSite=Lax so
// localhost auth works without TLS.
func setSessionCookie(c *gin.Context, token string, secure bool) {
	sameSite := http.SameSiteLaxMode
	if secure {
		sameSite = http.SameSiteNoneMode // cross-origin cookies require Secure + SameSite=None
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(tokenTTL.Seconds()),
		HttpOnly: true,
		SameSite: sameSite,
		Secure:   secure,
	})
}
