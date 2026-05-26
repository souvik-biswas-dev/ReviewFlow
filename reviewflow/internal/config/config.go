package config

import (
	"log"
	"os"
)

// Config holds all runtime configuration, loaded from environment variables.
//
// We deliberately avoid a third-party env library (e.g. godotenv): a tiny
// helper around os.Getenv keeps the dependency surface small and the behavior
// obvious. In Docker Compose the values arrive via `env_file: .env`; when
// running the binary directly, export the variables (or `source .env`) first.
type Config struct {
	Port        string // HTTP port the server listens on
	MongoURI    string // MongoDB connection string
	MongoDB     string // Database name to use
	Environment string // "development" | "production"
	CORSOrigin  string // Allowed browser origin (the SvelteKit dev server)

	// --- Auth / sessions ---
	JWTSecret string // HMAC secret used to sign session JWTs

	// --- GitHub OAuth (register at https://github.com/settings/developers) ---
	GitHubClientID     string
	GitHubClientSecret string
	GitHubRedirectURL  string // must exactly match the callback URL registered on GitHub
	FrontendURL        string // where the browser is sent after a successful login

	// --- AI (Google Gemini, free tier) ---
	GeminiAPIKey string // if empty, AI reviews are disabled (app still runs)
}

// Load reads configuration from the process environment, applying sane
// defaults for local development so the app boots with minimal config.
func Load() *Config {
	cfg := &Config{
		Port:        getEnv("PORT", "8080"),
		MongoURI:    getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:     getEnv("MONGO_DB", "reviewflow"),
		Environment: getEnv("ENVIRONMENT", "development"),
		CORSOrigin:  getEnv("CORS_ORIGIN", "http://localhost:5173"),

		JWTSecret:          getEnv("JWT_SECRET", "dev-insecure-secret-change-me"),
		GitHubClientID:     getEnv("GITHUB_CLIENT_ID", ""),
		GitHubClientSecret: getEnv("GITHUB_CLIENT_SECRET", ""),
		GitHubRedirectURL:  getEnv("GITHUB_REDIRECT_URL", "http://localhost:8080/auth/github/callback"),
		FrontendURL:        getEnv("FRONTEND_URL", "http://localhost:5173"),

		GeminiAPIKey: getEnv("GEMINI_API_KEY", ""),
	}

	// Fail/warn loudly for anything that makes auth insecure or non-functional,
	// so problems surface at startup rather than as confusing 401s later.
	if cfg.Environment == "production" && cfg.JWTSecret == "dev-insecure-secret-change-me" {
		log.Fatal("config: JWT_SECRET must be set to a strong value in production")
	}
	if cfg.GitHubClientID == "" || cfg.GitHubClientSecret == "" {
		log.Println("config: GITHUB_CLIENT_ID/SECRET not set — /auth/github will not work until configured")
	}
	if cfg.GeminiAPIKey == "" {
		log.Println("config: GEMINI_API_KEY not set — AI code reviews are disabled")
	}

	return cfg
}

// getEnv returns the value of the named env var, or fallback if it is unset or
// empty. We log when a default is applied so misconfiguration is visible early.
func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	log.Printf("config: %s not set, using default %q", key, fallback)
	return fallback
}
