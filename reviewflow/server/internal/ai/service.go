package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"reviewflow/internal/domain"
	"reviewflow/internal/ws"
)

var (
	ErrQueueFull = errors.New("ai: review queue is full, try again shortly")
)

// Store is the persistence the pipeline needs (satisfied by *store.Store).
type Store interface {
	MarkAIPending(ctx context.Context, snippetID string) error
	SaveAIReady(ctx context.Context, r *domain.AIReview) (*domain.AIReview, error)
	MarkAIFailed(ctx context.Context, snippetID, reason string) error
	PendingAISnippets(ctx context.Context, limit int) ([]string, error)
}

// Source loads snippet text for recovery after a restart.
type Source interface {
	SnippetSource(ctx context.Context, id string) (language, code string, err error)
}

// Publisher pushes a frame to a snippet's WebSocket room (satisfied by *ws.Hub).
type Publisher interface {
	BroadcastToRoom(room string, msg ws.Message)
}

type Config struct {
	RequestsPerMinute int           // self-imposed limit (default 10; free tier is 15)
	Timeout           time.Duration // budget for the model call + its single quota retry
	DefaultRetryDelay time.Duration // used when Gemini gives no RetryInfo
	QueueSize         int
	Workers           int
}

func (c *Config) defaults() {
	if c.RequestsPerMinute <= 0 {
		c.RequestsPerMinute = 10
	}
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.DefaultRetryDelay <= 0 {
		c.DefaultRetryDelay = 4 * time.Second
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 200
	}
	if c.Workers <= 0 {
		c.Workers = 2
	}
}

type job struct {
	snippetID string
	language  string
	code      string
}

// Service runs reviews off the request path.
type Service struct {
	cfg     Config
	gen     Generator
	store   Store
	pub     Publisher
	limiter *rate.Limiter
	queue   chan job

	mu       sync.Mutex
	inflight map[string]struct{}
	wg       sync.WaitGroup
}

func NewService(cfg Config, gen Generator, st Store, pub Publisher) *Service {
	cfg.defaults()
	// One token every 60s/N with burst 1 spaces calls evenly, so the per-minute
	// ceiling holds even for the very first requests (a larger burst would let
	// 2N calls through in the first minute).
	every := time.Minute / time.Duration(cfg.RequestsPerMinute)
	return newService(cfg, gen, st, pub, rate.NewLimiter(rate.Every(every), 1))
}

func newService(cfg Config, gen Generator, st Store, pub Publisher, lim *rate.Limiter) *Service {
	cfg.defaults()
	return &Service{
		cfg: cfg, gen: gen, store: st, pub: pub, limiter: lim,
		queue:    make(chan job, cfg.QueueSize),
		inflight: make(map[string]struct{}),
	}
}

// Start launches the workers; they stop when ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	for i := 0; i < s.cfg.Workers; i++ {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case j := <-s.queue:
					s.process(ctx, j)
				}
			}
		}()
	}
}

// Wait blocks until workers have exited (after the Start ctx is cancelled).
func (s *Service) Wait() { s.wg.Wait() }

// Enqueue records the snippet as pending and queues it. It never blocks on
// Gemini. Calling it again while the snippet is already queued is a no-op, so
// double-clicking "retry" is harmless.
func (s *Service) Enqueue(ctx context.Context, snippetID, language, code string) error {
	s.mu.Lock()
	if _, busy := s.inflight[snippetID]; busy {
		s.mu.Unlock()
		return nil
	}
	s.inflight[snippetID] = struct{}{}
	s.mu.Unlock()

	if err := s.store.MarkAIPending(ctx, snippetID); err != nil {
		s.clear(snippetID)
		return fmt.Errorf("ai: mark pending: %w", err)
	}
	select {
	case s.queue <- job{snippetID: snippetID, language: language, code: code}:
		s.pub.BroadcastToRoom(snippetID, ws.NewMessage(ws.TypeAIReviewPending, snippetID, ws.AIPending{SnippetID: snippetID}))
		return nil
	default:
		s.clear(snippetID)
		s.fail(ctx, snippetID, ErrQueueFull.Error())
		return ErrQueueFull
	}
}

// Recover re-queues reviews that were pending when the server last stopped.
func (s *Service) Recover(ctx context.Context, src Source) {
	ids, err := s.store.PendingAISnippets(ctx, 100)
	if err != nil {
		slog.Error("ai: recover", "err", err)
		return
	}
	for _, id := range ids {
		lang, code, err := src.SnippetSource(ctx, id)
		if err != nil {
			s.fail(ctx, id, "snippet no longer available")
			continue
		}
		if err := s.Enqueue(ctx, id, lang, code); err != nil {
			slog.Warn("ai: recover enqueue", "snippet", id, "err", err)
		}
	}
	if len(ids) > 0 {
		slog.Info("ai: re-queued unfinished reviews", "count", len(ids))
	}
}

func (s *Service) clear(id string) {
	s.mu.Lock()
	delete(s.inflight, id)
	s.mu.Unlock()
}

func (s *Service) process(ctx context.Context, j job) {
	defer s.clear(j.snippetID)

	// 1. Pace ourselves. Wait (not reject): a burst of snippets queues up and
	//    each one still gets its review, just a few seconds apart.
	if err := s.limiter.Wait(ctx); err != nil {
		return // shutting down; the row stays pending and Recover re-queues it
	}

	// 2. One budget covers the model call and its single quota retry.
	gctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	raw, err := s.generateWithRetry(gctx, UserPrompt(j.language, j.code))
	if err != nil {
		if ctx.Err() != nil {
			return // shutdown, not a real failure
		}
		s.fail(ctx, j.snippetID, userMessage(err, s.cfg.Timeout))
		slog.Warn("ai: generate failed", "snippet", j.snippetID, "err", err)
		return
	}

	// 3. Strict validation.
	review, err := ParseReview(raw, j.snippetID, j.language)
	if err != nil {
		s.fail(ctx, j.snippetID, "The model returned an unusable answer ("+err.Error()+"). Try again.")
		slog.Warn("ai: parse failed", "snippet", j.snippetID, "err", err)
		return
	}

	// 4. Idempotent upsert keyed by snippetId, then 5. broadcast.
	sctx, scancel := context.WithTimeout(ctx, 10*time.Second)
	defer scancel()
	saved, err := s.store.SaveAIReady(sctx, review)
	if err != nil {
		s.fail(ctx, j.snippetID, "Could not save the review.")
		slog.Error("ai: save failed", "snippet", j.snippetID, "err", err)
		return
	}
	s.pub.BroadcastToRoom(j.snippetID, ws.NewMessage(ws.TypeAIReviewReady, j.snippetID, saved))
	slog.Info("ai: review ready", "snippet", j.snippetID, "score", saved.QualityScore)
}

// generateWithRetry calls the model once and, on a quota error, waits out the
// back-off and tries exactly one more time (also through the limiter).
func (s *Service) generateWithRetry(ctx context.Context, prompt string) (string, error) {
	out, err := s.gen.Generate(ctx, SystemPrompt, prompt)
	var rl *RateLimitError
	if err == nil || !errors.As(err, &rl) {
		return out, err
	}

	delay := rl.RetryAfter
	if delay <= 0 {
		delay = s.cfg.DefaultRetryDelay
	}
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < delay+time.Second {
		return "", err // the retry could not finish inside the budget
	}
	slog.Warn("ai: gemini quota exhausted, retrying once", "after", delay)
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if werr := s.limiter.Wait(ctx); werr != nil {
		return "", werr
	}
	return s.gen.Generate(ctx, SystemPrompt, prompt)
}

func (s *Service) fail(ctx context.Context, snippetID, reason string) {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.store.MarkAIFailed(fctx, snippetID, reason); err != nil {
		slog.Error("ai: mark failed", "snippet", snippetID, "err", err)
	}
	s.pub.BroadcastToRoom(snippetID, ws.NewMessage(ws.TypeAIReviewFailed, snippetID, ws.AIFailure{SnippetID: snippetID, Error: reason}))
}

// userMessage turns pipeline errors into something a person can act on.
func userMessage(err error, budget time.Duration) string {
	var rl *RateLimitError
	var api *APIError
	switch {
	case errors.As(err, &rl):
		return "Gemini's quota is exhausted right now. Wait a minute and press Retry."
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("Gemini did not answer within %s. Press Retry.", budget)
	case errors.As(err, &api) && api.Status == 404:
		return "Gemini model not found. Set GEMINI_MODEL to a currently available model."
	case errors.As(err, &api) && (api.Status == 400 || api.Status == 401 || api.Status == 403):
		return fmt.Sprintf("Gemini rejected the request (HTTP %d). Check GEMINI_API_KEY and GEMINI_MODEL.", api.Status)
	default:
		return "Gemini request failed: " + err.Error()
	}
}
