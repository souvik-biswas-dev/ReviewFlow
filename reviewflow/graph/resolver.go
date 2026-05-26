package graph

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require
// here.

import (
	"reviewflow/internal/ai"
	"reviewflow/internal/config"
	"reviewflow/internal/db"
	"reviewflow/internal/ws"
)

// Resolver is the root resolver and dependency container. Wired in
// internal/router as &graph.Resolver{DB: ..., Cfg: ..., Hub: ..., AI: ...}.
//
// Resolver methods (in schema.resolvers.go) stay thin and delegate the real
// work to the graph/resolvers package, keeping generated and hand-written code
// cleanly separated.
type Resolver struct {
	DB  *db.Client
	Cfg *config.Config
	Hub *ws.Hub        // real-time fan-out (review_added / ai_review_ready / presence)
	AI  *ai.AIService  // nil when GEMINI_API_KEY is unset (AI reviews disabled)
}
