package graph

import (
	"context"
	"errors"
	"log/slog"

	"reviewflow/internal/auth"
	"reviewflow/internal/domain"
	"reviewflow/internal/store"
	"reviewflow/internal/ws"
)

// Publisher is how the GraphQL layer reaches the WebSocket hub (*ws.Hub).
type Publisher interface {
	BroadcastToRoom(room string, msg ws.Message)
}

// AIQueue is the review pipeline (*ai.Service). A nil AIQueue means AI is off.
type AIQueue interface {
	Enqueue(ctx context.Context, snippetID, language, code string) error
}

// Resolver is the dependency container for all resolvers.
type Resolver struct {
	Store *store.Store
	Hub   Publisher
	AI    AIQueue
}

// Limits keep one request from carrying an absurd payload.
const (
	maxTitle  = 200
	maxLang   = 40
	maxCode   = 400_000
	maxReview = 10_000
)

var (
	errUnauthenticated = errors.New("sign in to do that")
	errNotFound        = errors.New("snippet not found")
)

func caller(ctx context.Context) (auth.Identity, error) {
	id, ok := auth.FromContext(ctx)
	if !ok {
		return auth.Identity{}, errUnauthenticated
	}
	return id, nil
}

// decorate fixes up aiStatus for servers with AI switched off.
func (r *Resolver) decorate(sn *domain.Snippet) *domain.Snippet {
	if r.AI == nil && sn.AIStatus != domain.AIStatusReady {
		sn.AIStatus = domain.AIStatusDisabled
		sn.AIError = nil
	}
	return sn
}

// publish is nil-safe so resolvers work without a hub (tests, tooling).
func (r *Resolver) publish(room string, msg ws.Message) {
	if r.Hub != nil {
		r.Hub.BroadcastToRoom(room, msg)
	}
}

func logErr(msg string, err error, args ...any) {
	slog.Error(msg, append(args, "err", err)...)
}
