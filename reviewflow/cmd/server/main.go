package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"reviewflow/internal/ai"
	"reviewflow/internal/config"
	"reviewflow/internal/db"
	"reviewflow/internal/router"	
	"reviewflow/internal/ws"
)

func main() {
	// 1. Configuration -------------------------------------------------------
	cfg := config.Load()

	// 2. Database ------------------------------------------------------------
	// Fail fast: a server that can't reach MongoDB shouldn't accept traffic.
	database, err := db.Connect(cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatalf("startup: %v", err)
	}

	// Create required indexes before serving. Idempotent, so safe every boot.
	idxCtx, cancelIdx := context.WithTimeout(context.Background(), 10*time.Second)
	if err := database.EnsureIndexes(idxCtx); err != nil {
		cancelIdx()
		log.Fatalf("startup: ensure indexes: %v", err)
	}
	cancelIdx()

	// 3. Real-time hub + AI service -----------------------------------------
	// The hub's Run() goroutine owns all room-map mutations for its lifetime.
	hub := ws.NewHub()
	go hub.Run()

	// AI is optional: with no API key the app runs fine, snippets just never get
	// an AI review (aiReview stays null).
	var aiService *ai.AIService
	if cfg.GeminiAPIKey != "" {
		aiService, err = ai.NewAIService(context.Background(), cfg.GeminiAPIKey, database, hub)
		if err != nil {
			log.Fatalf("startup: ai service: %v", err)
		}
		defer aiService.Close()
	}

	// 4. HTTP server ---------------------------------------------------------
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router.New(cfg, database, hub, aiService),
		// Guards against slow-loris style stalls on the request headers.
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Run ListenAndServe in its own goroutine so main can block on signals.
	go func() {
		log.Printf("server: listening on :%s (env=%s)", cfg.Port, cfg.Environment)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	// 5. Graceful shutdown ---------------------------------------------------
	// Block until an interrupt (Ctrl+C) or terminate signal (Docker/orchestrator
	// stop) arrives.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("server: shutdown signal received")

	// Give in-flight requests up to 10s to complete before forcing exit.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server: forced shutdown: %v", err)
	}

	// Close DB connections last, once the server is no longer serving requests.
	if err := database.Disconnect(ctx); err != nil {
		log.Printf("db: disconnect error: %v", err)
	}

	log.Println("server: stopped cleanly")
}
