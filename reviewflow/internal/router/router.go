package router

import (
	"context"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"reviewflow/graph"
	"reviewflow/internal/ai"
	"reviewflow/internal/auth"
	"reviewflow/internal/config"
	"reviewflow/internal/db"
	"reviewflow/internal/notifications"
	"reviewflow/internal/ws"
)

// New builds the Gin engine with global middleware and all routes wired up. The
// hub and (optional) AI service are created in main so it can own their
// lifecycle (hub goroutine, ai client Close).
func New(cfg *config.Config, database *db.Client, hub *ws.Hub, aiSvc *ai.AIService) *gin.Engine {
	// Silence Gin's debug logging/warnings outside of development.
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// gin.New() rather than gin.Default() so we control exactly which
	// middleware runs and in what order.
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// CORS: the SvelteKit dev server (default :5173) calls this API from the
	// browser. AllowCredentials is required so the session cookie is sent, and
	// it forbids the "*" wildcard — hence an explicit origin.
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{cfg.CORSOrigin},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	registerHealth(r, database)
	registerAuth(r, cfg, database)
	registerGraphQL(r, cfg, database, hub, aiSvc)
	registerWebSocket(r, cfg, hub)
	registerNotifications(r, cfg, database)

	return r
}

// registerNotifications wires the bell-icon endpoints, all behind AuthMiddleware.
func registerNotifications(r *gin.Engine, cfg *config.Config, database *db.Client) {
	h := notifications.NewHandler(database)
	g := r.Group("/notifications", auth.AuthMiddleware(cfg.JWTSecret))
	g.GET("", h.List)
	g.POST("/read", h.MarkAllRead)
}

// registerHealth wires GET and HEAD /health with a live DB ping.
// Gin does not automatically handle HEAD for GET routes, so we register both.
// UptimeRobot (and many other monitors) default to HEAD requests.
func registerHealth(r *gin.Engine, database *db.Client) {
	r.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		if err := database.Mongo.Ping(ctx, readpref.Primary()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "db": "disconnected"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "db": "connected"})
	})
	// HEAD variant: same liveness check, no body (HEAD responses must omit body).
	r.HEAD("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		if err := database.Mongo.Ping(ctx, readpref.Primary()); err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.Status(http.StatusOK)
	})
}

// registerAuth wires the GitHub OAuth + session endpoints.
func registerAuth(r *gin.Engine, cfg *config.Config, database *db.Client) {
	h := auth.NewHandler(cfg, database)
	r.GET("/auth/github", h.GitHubLogin)
	r.GET("/auth/github/callback", h.GitHubCallback)
	// /auth/me requires a valid session cookie.
	r.GET("/auth/me", auth.AuthMiddleware(cfg.JWTSecret), h.Me)
}

// registerGraphQL wires the GraphQL execution endpoint and the playground.
func registerGraphQL(r *gin.Engine, cfg *config.Config, database *db.Client, hub *ws.Hub, aiSvc *ai.AIService) {
	resolver := &graph.Resolver{DB: database, Cfg: cfg, Hub: hub, AI: aiSvc}

	// NewDefaultServer sets up the common HTTP/WS transports + introspection.
	srv := handler.NewDefaultServer(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))

	// GraphQLContext is soft auth: it injects the user id into the request
	// context when a valid cookie is present, but never rejects — resolvers
	// decide which operations require authentication.
	r.POST("/graphql", auth.GraphQLContext(cfg.JWTSecret), gin.WrapH(srv))

	// Interactive query explorer, served at GET /graphql/playground.
	r.GET("/graphql/playground", gin.WrapH(playground.Handler("ReviewFlow", "/graphql")))
}

// registerWebSocket wires the real-time endpoint. AuthMiddleware validates the
// JWT cookie BEFORE the connection is upgraded.
func registerWebSocket(r *gin.Engine, cfg *config.Config, hub *ws.Hub) {
	h := ws.NewHandler(hub, cfg.CORSOrigin)
	r.GET("/ws/:snippetId", auth.AuthMiddleware(cfg.JWTSecret), h.ServeWS)
}
