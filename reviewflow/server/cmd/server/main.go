// Command server runs the ReviewFlow backend: Gin HTTP + GraphQL + WebSocket
// hub + Gemini review pipeline, backed by MongoDB.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"reviewflow/internal/ai"
	"reviewflow/internal/api"
	"reviewflow/internal/config"
	"reviewflow/internal/graph"
	"reviewflow/internal/store"
	"reviewflow/internal/ws"
)

const shutdownGrace = 10 * time.Second

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

type sourceAdapter struct{ st *store.Store }

func (a sourceAdapter) SnippetSource(ctx context.Context, id string) (string, string, error) {
	s, err := a.st.SnippetSource(ctx, id)
	if err != nil {
		return "", "", err
	}
	return s.Language, s.Code, nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Database: fail fast, then make sure indexes exist.
	st, err := store.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		return err
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = st.Close(cctx)
	}()
	ictx, icancel := context.WithTimeout(ctx, 15*time.Second)
	err = st.EnsureIndexes(ictx)
	icancel()
	if err != nil {
		return err
	}
	slog.Info("db: connected", "database", cfg.MongoDB)

	// Realtime hub: one owner goroutine.
	hub := ws.NewHub()
	go hub.Run(ctx)

	// AI pipeline (optional).
	var queue graph.AIQueue
	var aiSvc *ai.Service
	if cfg.AIEnabled() {
		aiSvc = ai.NewService(ai.Config{
			RequestsPerMinute: cfg.AIRequestsPerMinute,
			Timeout:           cfg.AITimeout,
		}, ai.NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiModel, cfg.GeminiBaseURL), st, hub)
		aiSvc.Start(ctx)
		aiSvc.Recover(ctx, sourceAdapter{st})
		queue = aiSvc
		slog.Info("ai: enabled", "model", cfg.GeminiModel, "rpm", cfg.AIRequestsPerMinute, "timeout", cfg.AITimeout)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewRouter(api.Deps{Config: cfg, Store: st, Hub: hub, AI: queue}),
		ReadHeaderTimeout: 5 * time.Second, // slow-loris guard; no WriteTimeout (WebSockets)
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("server: listening", "addr", srv.Addr, "env", cfg.Environment)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("server: shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	_ = srv.Shutdown(sctx) // stops accepting; hijacked sockets are closed by the hub below
	stop()                 // cancels ctx: hub closes every socket, AI workers drain
	<-hub.Done()
	if aiSvc != nil {
		aiSvc.Wait()
	}
	slog.Info("server: stopped cleanly")
	return nil
}
