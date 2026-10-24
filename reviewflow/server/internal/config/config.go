// Package config reads runtime configuration from environment variables.
//
// Unlike the Node version, a Go binary does not load .env files on its own, so
// this package includes a tiny loader: `go run ./cmd/server` from reviewflow/server
// picks up reviewflow/.env automatically, and real environment variables always win.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const InsecureDevSecret = "dev-insecure-secret-change-me"

type Config struct {
	Port        string
	Environment string // "development" | "production"

	// CORSOrigins are the browser origins allowed to call the API and open
	// WebSockets. FRONTEND_URL's origin is always included.
	CORSOrigins []string

	MongoURI string
	MongoDB  string

	JWTSecret string

	GithubClientID     string
	GithubClientSecret string
	GithubRedirectURL  string
	FrontendURL        string

	// AllowGuestLogin enables POST /auth/guest: pick a display name and get a
	// session without GitHub. Defaults to on in development, off in production.
	AllowGuestLogin bool

	GeminiAPIKey  string
	GeminiModel   string
	GeminiBaseURL string
	// AIRequestsPerMinute is the self-imposed Gemini rate limit (free tier is 15).
	AIRequestsPerMinute int
	// AITimeout bounds one review (the model call plus its single quota retry).
	AITimeout time.Duration

	// StaticDir, when set, makes the Go server also serve the built SvelteKit
	// SPA (web/build) with index.html fallback. Handy for single-process demos.
	StaticDir string
}

func (c *Config) IsProduction() bool  { return c.Environment == "production" }
func (c *Config) GitHubEnabled() bool { return c.GithubClientID != "" && c.GithubClientSecret != "" }
func (c *Config) AIEnabled() bool     { return c.GeminiAPIKey != "" }

// Load reads .env files (without overriding real env vars) and then the
// environment. It returns an error for values that would make the server
// insecure or unable to start, so problems surface at boot, not as mystery 401s.
func Load() (*Config, error) {
	loadDotEnv(".env", "../.env")

	c := &Config{
		Port:        get("PORT", "8080"),
		Environment: strings.ToLower(get("ENVIRONMENT", "development")),
		MongoURI:    get("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:     get("MONGO_DB", "reviewflow"),
		JWTSecret:   get("JWT_SECRET", InsecureDevSecret),

		GithubClientID:     get("GITHUB_CLIENT_ID", ""),
		GithubClientSecret: get("GITHUB_CLIENT_SECRET", ""),
		GithubRedirectURL:  get("GITHUB_REDIRECT_URL", "http://localhost:8080/auth/github/callback"),
		FrontendURL:        strings.TrimRight(get("FRONTEND_URL", "http://localhost:5173"), "/"),

		GeminiAPIKey:  get("GEMINI_API_KEY", ""),
		GeminiModel:   get("GEMINI_MODEL", "gemini-3.5-flash"),
		GeminiBaseURL: strings.TrimRight(get("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"), "/"),

		StaticDir: get("STATIC_DIR", ""),
	}

	if p, err := strconv.Atoi(c.Port); err != nil || p <= 0 || p > 65535 {
		return nil, fmt.Errorf("PORT must be a valid port number, got %q", c.Port)
	}
	if c.Environment != "development" && c.Environment != "production" {
		return nil, fmt.Errorf("ENVIRONMENT must be development or production, got %q", c.Environment)
	}

	var err error
	if c.AIRequestsPerMinute, err = getInt("AI_REQUESTS_PER_MINUTE", 10); err != nil {
		return nil, err
	}
	if c.AIRequestsPerMinute <= 0 {
		return nil, errors.New("AI_REQUESTS_PER_MINUTE must be positive")
	}
	secs, err := getInt("AI_TIMEOUT_SECONDS", 30)
	if err != nil {
		return nil, err
	}
	if secs <= 0 {
		return nil, errors.New("AI_TIMEOUT_SECONDS must be positive")
	}
	c.AITimeout = time.Duration(secs) * time.Second

	guestDefault := !c.IsProduction()
	if c.AllowGuestLogin, err = getBool("ALLOW_GUEST_LOGIN", guestDefault); err != nil {
		return nil, err
	}

	c.CORSOrigins = originList(get("CORS_ORIGIN", "http://localhost:5173,http://127.0.0.1:5173"), c.FrontendURL)

	if c.IsProduction() {
		if c.JWTSecret == InsecureDevSecret || len(c.JWTSecret) < 32 {
			return nil, errors.New("JWT_SECRET must be set to a random string of at least 32 characters in production (openssl rand -hex 32)")
		}
	}
	if !c.GitHubEnabled() && !c.AllowGuestLogin {
		slog.Warn("config: neither GitHub OAuth nor guest login is enabled; nobody will be able to sign in")
	}
	if !c.GitHubEnabled() {
		slog.Info("config: GITHUB_CLIENT_ID/SECRET not set, GitHub sign-in disabled")
	}
	if !c.AIEnabled() {
		slog.Info("config: GEMINI_API_KEY not set, AI reviews disabled (everything else works)")
	}
	return c, nil
}

func get(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func getInt(key string, def int) (int, error) {
	v := get(key, "")
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, v)
	}
	return n, nil
}

func getBool(key string, def bool) (bool, error) {
	v := get(key, "")
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false, got %q", key, v)
	}
	return b, nil
}

// originList splits a comma-separated origin list, normalises it, and makes
// sure the frontend's own origin is present.
func originList(csv, frontendURL string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(o string) {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o != "" && !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	for _, o := range strings.Split(csv, ",") {
		add(o)
	}
	if u, err := url.Parse(frontendURL); err == nil && u.Scheme != "" && u.Host != "" {
		add(u.Scheme + "://" + u.Host)
	}
	return out
}

// loadDotEnv reads KEY=VALUE lines from the first files that exist. Variables
// already present in the real environment are never overridden.
func loadDotEnv(paths ...string) {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, ok := parseEnvLine(sc.Text())
			if !ok {
				continue
			}
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
		_ = f.Close()
	}
}

func parseEnvLine(line string) (key, val string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	k, v, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	k, v = strings.TrimSpace(k), strings.TrimSpace(v)
	if k == "" {
		return "", "", false
	}
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		v = v[1 : len(v)-1]
	} else if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i]) // strip trailing comment on unquoted values
	}
	return k, v, true
}
