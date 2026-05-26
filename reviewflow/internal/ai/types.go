package ai

import (
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AIReviewsCollection is the MongoDB collection where generated reviews live.
const AIReviewsCollection = "ai_reviews"

// Caps enforced on the model's output (also requested in the prompt, but we
// clamp defensively in case the model overshoots).
const (
	maxSuggestions   = 5
	maxRefactorHints = 3
)

// AIReview is both the MongoDB document (bson tags) and the JSON payload sent
// over the WebSocket "ai_review_ready" event (json tags). The GraphQL mapping
// lives in the graph/resolvers package so this type stays transport-agnostic.
type AIReview struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	SnippetID     string             `bson:"snippetId" json:"snippetId"`
	Suggestions   []string           `bson:"suggestions" json:"suggestions"`
	Complexity    string             `bson:"complexity" json:"complexity"`
	RefactorHints []string           `bson:"refactorHints" json:"refactorHints"`
	SecurityFlags []string           `bson:"securityFlags" json:"securityFlags"`
	QualityScore  int                `bson:"qualityScore" json:"qualityScore"`
	Language      string             `bson:"language" json:"language"`
	GeneratedAt   time.Time          `bson:"generatedAt" json:"generatedAt"`
}

// geminiReview is the exact JSON shape we instruct Gemini to return. It carries
// only the model-authored fields; we add snippetId/language/generatedAt
// ourselves so the model can't get them wrong.
type geminiReview struct {
	Suggestions   []string `json:"suggestions"`
	Complexity    string   `json:"complexity"`
	RefactorHints []string `json:"refactorHints"`
	SecurityFlags []string `json:"securityFlags"`
	QualityScore  int      `json:"qualityScore"`
}

// toAIReview builds the stored/served review, normalizing the model output:
// nil slices become empty (so GraphQL's non-null lists are satisfied) and the
// length caps are enforced.
func (g geminiReview) toAIReview(snippetID, language string) *AIReview {
	return &AIReview{
		SnippetID:     snippetID,
		Suggestions:   clampStrings(g.Suggestions, maxSuggestions),
		Complexity:    g.Complexity,
		RefactorHints: clampStrings(g.RefactorHints, maxRefactorHints),
		SecurityFlags: nonNil(g.SecurityFlags),
		QualityScore:  g.QualityScore,
		Language:      language,
		GeneratedAt:   time.Now().UTC(),
	}
}

// Validate enforces the invariants we promise the rest of the system: a quality
// score in [1,10] and a non-empty complexity estimate.
func (r *AIReview) Validate() error {
	if r.QualityScore < 1 || r.QualityScore > 10 {
		return fmt.Errorf("qualityScore %d out of range 1-10", r.QualityScore)
	}
	if r.Complexity == "" {
		return errors.New("complexity is empty")
	}
	return nil
}

// nonNil returns s, or an empty (non-nil) slice if s is nil.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// clampStrings returns at most max non-nil elements.
func clampStrings(s []string, max int) []string {
	s = nonNil(s)
	if len(s) > max {
		return s[:max]
	}
	return s
}
