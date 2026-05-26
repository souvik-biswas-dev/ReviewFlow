package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/time/rate"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"reviewflow/internal/db"
	"reviewflow/internal/ws"
)

const (
	modelName = "gemini-2.0-flash"

	// reviewTimeout bounds a single Gemini call (incl. the retry's own call).
	reviewTimeout = 30 * time.Second
	// retryDelay is the back-off before the single retry on a rate-limit error.
	retryDelay = 4 * time.Second

	// Free tier allows 15 RPM; we self-limit to 10 RPM to stay comfortably under.
	requestsPerMinute = 10
)

// ErrRateLimited is returned when our local limiter rejects a call (we're
// pacing ourselves under the free-tier quota). The frontend can surface this as
// "AI is busy, try again shortly".
var ErrRateLimited = errors.New("ai: rate limited, try again shortly")

// contentGenerator is the seam we mock in tests. The real implementation talks
// to Gemini; tests inject a fake that returns canned JSON.
type contentGenerator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// AIService orchestrates the review pipeline: rate-limit -> generate -> parse
// -> persist -> broadcast.
type AIService struct {
	client  *genai.Client // nil in tests
	gen     contentGenerator
	db      *db.Client // nil-safe: skipped when nil (tests)
	hub     *ws.Hub    // nil-safe: skipped when nil (tests)
	limiter *rate.Limiter
}

// NewAIService builds the production service: a Gemini client configured with
// the strict system prompt and JSON response mode.
func NewAIService(ctx context.Context, apiKey string, database *db.Client, hub *ws.Hub) (*AIService, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("ai: create genai client: %w", err)
	}

	model := client.GenerativeModel(modelName)
	model.SystemInstruction = &genai.Content{Parts: []genai.Part{genai.Text(SystemPrompt)}}
	model.ResponseMIMEType = "application/json" // force JSON, not prose
	model.SetTemperature(0.2)                   // low temp: deterministic, analytical

	return &AIService{
		client:  client,
		gen:     &geminiGenerator{model: model},
		db:      database,
		hub:     hub,
		limiter: rate.NewLimiter(rate.Every(time.Minute/requestsPerMinute), 1),
	}, nil
}

// Close releases the underlying Gemini client. Call on shutdown.
func (s *AIService) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// ReviewCode runs the full pipeline for one snippet and returns the review.
//
// Steps: local rate-limit check -> Gemini call (30s budget, one retry on a
// remote rate-limit) -> parse + validate -> save to ai_reviews -> broadcast
// "ai_review_ready". It is safe to call from a goroutine.
func (s *AIService) ReviewCode(ctx context.Context, snippetID, language, code string) (*AIReview, error) {
	// Self-pace under the free-tier quota. We reject rather than block so the
	// caller (and ultimately the frontend) gets a clear, fast signal.
	if !s.limiter.Allow() {
		return nil, ErrRateLimited
	}

	callCtx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()

	raw, err := s.generateWithRetry(callCtx, BuildUserPrompt(language, code))
	if err != nil {
		return nil, fmt.Errorf("ai: generate: %w", err)
	}

	review, err := parseAndValidate(raw, snippetID, language)
	if err != nil {
		return nil, fmt.Errorf("ai: %w", err)
	}

	if err := s.save(ctx, review); err != nil {
		return nil, fmt.Errorf("ai: save: %w", err)
	}

	s.broadcast(review)
	return review, nil
}

// generateWithRetry calls the model once and, if it's a rate-limit error,
// waits retryDelay and tries exactly one more time.
func (s *AIService) generateWithRetry(ctx context.Context, prompt string) (string, error) {
	raw, err := s.gen.Generate(ctx, prompt)
	if err == nil {
		return raw, nil
	}
	if !isRateLimit(err) {
		return "", err
	}

	log.Printf("ai: rate limited by Gemini, retrying once in %s", retryDelay)
	select {
	case <-time.After(retryDelay):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return s.gen.Generate(ctx, prompt)
}

// save upserts the review keyed by snippetId, so a re-run replaces the old one.
func (s *AIService) save(ctx context.Context, r *AIReview) error {
	if s.db == nil {
		return nil // test mode
	}
	saveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	opts := options.Replace().SetUpsert(true)
	_, err := s.db.Database.Collection(AIReviewsCollection).
		ReplaceOne(saveCtx, bson.M{"snippetId": r.SnippetID}, r, opts)
	return err
}

// broadcast notifies everyone viewing the snippet that the AI review is ready.
func (s *AIService) broadcast(r *AIReview) {
	if s.hub == nil {
		return // test mode
	}
	msg, err := ws.NewMessage(ws.MessageAIReviewReady, r.SnippetID, r)
	if err != nil {
		log.Printf("ai: build broadcast for snippet %s: %v", r.SnippetID, err)
		return
	}
	s.hub.BroadcastToRoom(r.SnippetID, msg)
}

// parseAndValidate decodes the model's JSON into an AIReview and validates it.
// It defensively strips code fences in case the model wraps the JSON despite
// our instructions.
func parseAndValidate(raw, snippetID, language string) (*AIReview, error) {
	var g geminiReview
	if err := json.Unmarshal([]byte(stripCodeFences(raw)), &g); err != nil {
		return nil, fmt.Errorf("invalid JSON from model: %w", err)
	}
	review := g.toAIReview(snippetID, language)
	if err := review.Validate(); err != nil {
		return nil, err
	}
	return review, nil
}

// stripCodeFences removes a leading/trailing ```json ... ``` wrapper if present.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the first line (``` or ```json) and a trailing ``` line.
	if i := strings.IndexByte(s, '\n'); i != -1 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

// isRateLimit reports whether err is a Gemini rate-limit / quota error.
func isRateLimit(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.ResourceExhausted {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "resourceexhausted") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "quota") ||
		strings.Contains(msg, "429")
}

// geminiGenerator is the production contentGenerator backed by the SDK.
type geminiGenerator struct {
	model *genai.GenerativeModel
}

func (g *geminiGenerator) Generate(ctx context.Context, prompt string) (string, error) {
	resp, err := g.model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", err
	}
	text := extractText(resp)
	if text == "" {
		return "", errors.New("empty response from model (possibly blocked by safety filters)")
	}
	return text, nil
}

// extractText concatenates the text parts of the first candidate.
func extractText(resp *genai.GenerateContentResponse) string {
	var b strings.Builder
	for _, cand := range resp.Candidates {
		if cand.Content == nil {
			continue
		}
		for _, part := range cand.Content.Parts {
			if t, ok := part.(genai.Text); ok {
				b.WriteString(string(t))
			}
		}
	}
	return b.String()
}
