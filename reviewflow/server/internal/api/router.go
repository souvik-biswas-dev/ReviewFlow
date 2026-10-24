// Package api assembles the Gin engine: middleware, REST endpoints, GraphQL
// and the WebSocket upgrade route.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"reviewflow/internal/auth"
	"reviewflow/internal/config"
	"reviewflow/internal/graph"
	"reviewflow/internal/store"
	"reviewflow/internal/ws"
)

// bodyLimit comfortably fits a large source file plus its previous version.
const bodyLimit = 5 << 20

type Deps struct {
	Config *config.Config
	Store  *store.Store
	Hub    *ws.Hub
	AI     graph.AIQueue // nil when Gemini is not configured
}

func NewRouter(d Deps) *gin.Engine {
	cfg := d.Config
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(requestLogger(), gin.CustomRecovery(func(c *gin.Context, err any) {
		slog.Error("panic in handler", "err", err, "path", c.Request.URL.Path)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}))

	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.CORSOrigins,
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := d.Store.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "db": "disconnected"})
			return
		}
		rooms, conns := d.Hub.Stats()
		c.JSON(http.StatusOK, gin.H{"status": "ok", "db": "connected", "ai": d.AI != nil, "rooms": rooms, "connections": conns})
	})

	auth.NewHandler(cfg, d.Store).Register(r)
	registerGraphQL(r, d)
	registerNotifications(r, cfg, d.Store)

	r.GET("/ws/:snippetID", ws.Handler(ws.HandlerConfig{
		Hub: d.Hub,
		Authenticate: func(req *http.Request) (string, string, bool) {
			id, ok := auth.Authenticate(cfg.JWTSecret, req)
			return id.UserID, id.Username, ok
		},
		RoomExists:  d.Store.SnippetExists,
		CheckOrigin: ws.OriginChecker(cfg.CORSOrigins),
	}))

	if cfg.StaticDir != "" {
		serveSPA(r, cfg.StaticDir)
	} else {
		r.NoRoute(func(c *gin.Context) { c.JSON(http.StatusNotFound, gin.H{"error": "not found"}) })
	}
	return r
}

func registerGraphQL(r *gin.Engine, d Deps) {
	res := &graph.Resolver{Store: d.Store, Hub: d.Hub, AI: d.AI}
	srv := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: res}))
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.POST{})
	srv.Use(extension.FixedComplexityLimit(500))
	if !d.Config.IsProduction() {
		srv.Use(extension.Introspection{})
	}
	srv.SetRecoverFunc(func(ctx context.Context, err any) error {
		slog.Error("panic in resolver", "err", err)
		return errInternal
	})
	srv.SetErrorPresenter(graphql.DefaultErrorPresenter)

	// Soft auth: attach the caller if a valid token is present, never reject;
	// resolvers decide which operations need a login.
	r.POST("/graphql", auth.Soft(d.Config.JWTSecret), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, bodyLimit)
		srv.ServeHTTP(c.Writer, c.Request)
	})
	if !d.Config.IsProduction() {
		r.GET("/graphql/playground", gin.WrapH(playground.Handler("ReviewFlow", "/graphql")))
	}
}

type apiError string

func (e apiError) Error() string { return string(e) }

const errInternal = apiError("internal server error")

// serveSPA serves the built SvelteKit app and falls back to index.html so
// client-side routes survive a hard refresh.
func serveSPA(r *gin.Engine, dir string) {
	index := filepath.Join(dir, "index.html")
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if c.Request.Method != http.MethodGet || strings.HasPrefix(p, "/graphql") ||
			strings.HasPrefix(p, "/auth/") || strings.HasPrefix(p, "/ws/") || strings.HasPrefix(p, "/notifications") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		clean := filepath.Join(dir, filepath.Clean("/"+p))
		if st, err := os.Stat(clean); err == nil && !st.IsDir() {
			c.File(clean)
			return
		}
		c.File(index)
	})
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http", "status", c.Writer.Status(), "ms", time.Since(start).Milliseconds(),
			"method", c.Request.Method, "path", c.Request.URL.Path)
	}
}
