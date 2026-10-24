package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"reviewflow/internal/config"
	"reviewflow/internal/domain"
	"reviewflow/internal/store"
)

const (
	oauthStateCookie = "rf_oauth_state"
	githubTimeout    = 10 * time.Second
)

// guestName: 2-32 chars, letters/digits/_/-, starting with a letter or digit.
var guestName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,31}$`)

// Users is the slice of the store the auth handlers need.
type Users interface {
	UpsertUser(ctx context.Context, provider, providerID, username, avatarURL string) (*domain.User, error)
	GetUser(ctx context.Context, id string) (*domain.User, error)
}

type Handler struct {
	cfg   *config.Config
	users Users
	http  *http.Client

	// Overridable in tests.
	GitHubAuthorizeURL string
	GitHubTokenURL     string
	GitHubUserURL      string
}

func NewHandler(cfg *config.Config, users Users) *Handler {
	return &Handler{
		cfg:                cfg,
		users:              users,
		http:               &http.Client{Timeout: githubTimeout},
		GitHubAuthorizeURL: "https://github.com/login/oauth/authorize",
		GitHubTokenURL:     "https://github.com/login/oauth/access_token",
		GitHubUserURL:      "https://api.github.com/user",
	}
}

// Register mounts the /auth routes.
func (h *Handler) Register(r gin.IRouter) {
	g := r.Group("/auth")
	g.GET("/config", h.Config)
	g.GET("/github", h.GitHubLogin)
	g.GET("/github/callback", h.GitHubCallback)
	g.POST("/guest", h.Guest)
	g.POST("/logout", h.Logout)
	g.GET("/me", Required(h.cfg.JWTSecret), h.Me)
}

// Config tells the SPA which sign-in methods to offer.
func (h *Handler) Config(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"github": h.cfg.GitHubEnabled(),
		"guest":  h.cfg.AllowGuestLogin,
		"ai":     h.cfg.AIEnabled(),
	})
}

// GitHubLogin starts the OAuth flow with an anti-CSRF state cookie.
func (h *Handler) GitHubLogin(c *gin.Context) {
	if !h.cfg.GitHubEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GitHub sign-in is not configured on this server"})
		return
	}
	state, err := randomHex(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start login"})
		return
	}
	// Lax is enough: GitHub returns via a top-level GET navigation.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthStateCookie, state, 600, "/", "", h.cfg.IsProduction(), true)

	q := url.Values{
		"client_id":    {h.cfg.GithubClientID},
		"redirect_uri": {h.cfg.GithubRedirectURL},
		"scope":        {"read:user"},
		"state":        {state},
	}
	c.Redirect(http.StatusTemporaryRedirect, h.GitHubAuthorizeURL+"?"+q.Encode())
}

// GitHubCallback verifies state, exchanges the code, upserts the user, and
// sends the browser back to the SPA with the token in the URL *fragment*
// (fragments are never sent to servers or leaked via Referer).
func (h *Handler) GitHubCallback(c *gin.Context) {
	fail := func(msg string, status int, err error) {
		if err != nil {
			slog.Error("auth: github callback", "step", msg, "err", err)
		}
		_ = status
		c.Redirect(http.StatusTemporaryRedirect, h.frontendCallback("error", msg))
	}

	cookie, err := c.Cookie(oauthStateCookie)
	state := c.Query("state")
	if err != nil || cookie == "" || subtle.ConstantTimeCompare([]byte(cookie), []byte(state)) != 1 {
		fail("Sign-in expired or was tampered with. Please try again.", http.StatusBadRequest, nil)
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthStateCookie, "", -1, "/", "", h.cfg.IsProduction(), true)

	code := c.Query("code")
	if code == "" {
		fail("GitHub did not return an authorization code.", http.StatusBadRequest, nil)
		return
	}
	ghToken, err := h.exchangeCode(c.Request.Context(), code)
	if err != nil {
		fail("GitHub token exchange failed.", http.StatusBadGateway, err)
		return
	}
	profile, err := h.fetchProfile(c.Request.Context(), ghToken)
	if err != nil {
		fail("Could not read your GitHub profile.", http.StatusBadGateway, err)
		return
	}
	user, err := h.users.UpsertUser(c.Request.Context(), "github", strconv.FormatInt(profile.ID, 10), profile.Login, profile.AvatarURL)
	if err != nil {
		fail("Could not save your account.", http.StatusInternalServerError, err)
		return
	}
	token, err := h.issue(c, user)
	if err != nil {
		fail("Could not create a session.", http.StatusInternalServerError, err)
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, h.frontendCallback("token", token))
}

type guestRequest struct {
	Username string `json:"username"`
}

// Guest signs in with just a display name. Intended for local development and
// demos (two browsers, two names = a live multi-user review). Names are not
// protected: anyone can claim any guest name, so it is off in production.
func (h *Handler) Guest(c *gin.Context) {
	if !h.cfg.AllowGuestLogin {
		c.JSON(http.StatusForbidden, gin.H{"error": "guest login is disabled on this server"})
		return
	}
	var req guestRequest
	if err := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096)).Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected JSON body {\"username\": \"...\"}"})
		return
	}
	name := strings.TrimSpace(req.Username)
	if !guestName.MatchString(name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username must be 2-32 characters: letters, digits, _ or -"})
		return
	}
	user, err := h.users.UpsertUser(c.Request.Context(), "guest", strings.ToLower(name), name, "")
	if err != nil {
		slog.Error("auth: guest upsert", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create guest account"})
		return
	}
	token, err := h.issue(c, user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create a session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": user})
}

// Me returns the current user.
func (h *Handler) Me(c *gin.Context) {
	user, err := h.users.GetUser(c.Request.Context(), UserID(c))
	switch {
	case errors.Is(err, store.ErrInvalidID):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid user id in token"})
	case errors.Is(err, store.ErrNotFound):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
	case err != nil:
		slog.Error("auth: me", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	default:
		c.JSON(http.StatusOK, user)
	}
}

// Logout clears the HttpOnly session cookie. (The SPA also forgets its copy of
// the token; without this the cookie would keep the user signed in.)
func (h *Handler) Logout(c *gin.Context) {
	h.setSessionCookie(c, "", -1)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---------------------------------------------------------------------------

func (h *Handler) issue(c *gin.Context, user *domain.User) (string, error) {
	token, err := GenerateToken(h.cfg.JWTSecret, user.ID, user.GithubUsername)
	if err != nil {
		return "", err
	}
	h.setSessionCookie(c, token, int(TokenTTL.Seconds()))
	return token, nil
}

// In production the SPA and API may live on different sites, which needs
// SameSite=None; Secure. In development Lax works over plain http.
func (h *Handler) setSessionCookie(c *gin.Context, value string, maxAge int) {
	if h.cfg.IsProduction() {
		c.SetSameSite(http.SameSiteNoneMode)
	} else {
		c.SetSameSite(http.SameSiteLaxMode)
	}
	c.SetCookie(CookieName, value, maxAge, "/", "", h.cfg.IsProduction(), true)
}

func (h *Handler) frontendCallback(key, value string) string {
	return h.cfg.FrontendURL + "/login/callback#" + key + "=" + url.QueryEscape(value)
}

type githubProfile struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

func (h *Handler) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"client_id":     {h.cfg.GithubClientID},
		"client_secret": {h.cfg.GithubClientSecret},
		"code":          {code},
		"redirect_uri":  {h.cfg.GithubRedirectURL},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.GitHubTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
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
		return "", fmt.Errorf("decode github token response: %w", err)
	}
	if body.Error != "" {
		return "", fmt.Errorf("github oauth: %s: %s", body.Error, body.ErrorDesc)
	}
	if body.AccessToken == "" {
		return "", errors.New("github oauth: empty access token")
	}
	return body.AccessToken, nil
}

func (h *Handler) fetchProfile(ctx context.Context, token string) (*githubProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.GitHubUserURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github user: unexpected status %d", resp.StatusCode)
	}
	var p githubProfile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	if p.ID == 0 || p.Login == "" {
		return nil, errors.New("github user: incomplete profile")
	}
	return &p, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
