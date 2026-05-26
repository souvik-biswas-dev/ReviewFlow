package ai

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/time/rate"
)

// fakeGen is a contentGenerator that returns canned output, standing in for the
// real Gemini call so the parsing/validation pipeline can be tested offline.
type fakeGen struct {
	out string
	err error
}

func (f fakeGen) Generate(ctx context.Context, prompt string) (string, error) {
	return f.out, f.err
}

// newTestService builds an AIService with the given generator, an unlimited
// limiter, and nil db/hub (so save + broadcast are skipped).
func newTestService(gen contentGenerator) *AIService {
	return &AIService{
		gen:     gen,
		limiter: rate.NewLimiter(rate.Inf, 1),
	}
}

func TestReviewCode(t *testing.T) {
	const validJSON = `{
		"suggestions": ["use errors.Is for sentinel checks", "handle io.EOF explicitly", "pass a context with timeout"],
		"complexity": "O(n) time, O(1) space",
		"refactorHints": ["extract the parse loop into its own function", "rename x to itemCount"],
		"securityFlags": [],
		"qualityScore": 8
	}`

	t.Run("parses a valid response", func(t *testing.T) {
		svc := newTestService(fakeGen{out: validJSON})

		got, err := svc.ReviewCode(context.Background(), "snip1", "go", "package main")
		if err != nil {
			t.Fatalf("ReviewCode returned error: %v", err)
		}
		if got.QualityScore != 8 {
			t.Errorf("QualityScore = %d, want 8", got.QualityScore)
		}
		if got.Complexity != "O(n) time, O(1) space" {
			t.Errorf("Complexity = %q", got.Complexity)
		}
		if len(got.Suggestions) != 3 {
			t.Errorf("len(Suggestions) = %d, want 3", len(got.Suggestions))
		}
		if len(got.RefactorHints) != 2 {
			t.Errorf("len(RefactorHints) = %d, want 2", len(got.RefactorHints))
		}
		// Metadata is set by us, not the model.
		if got.SnippetID != "snip1" || got.Language != "go" {
			t.Errorf("metadata = (%q,%q), want (snip1, go)", got.SnippetID, got.Language)
		}
		if got.GeneratedAt.IsZero() {
			t.Error("GeneratedAt not set")
		}
		// Non-null list invariant.
		if got.SecurityFlags == nil {
			t.Error("SecurityFlags is nil, want non-nil empty slice")
		}
	})

	t.Run("strips markdown code fences", func(t *testing.T) {
		fenced := "```json\n" + validJSON + "\n```"
		svc := newTestService(fakeGen{out: fenced})

		got, err := svc.ReviewCode(context.Background(), "snip2", "go", "code")
		if err != nil {
			t.Fatalf("ReviewCode returned error: %v", err)
		}
		if got.QualityScore != 8 {
			t.Errorf("QualityScore = %d, want 8", got.QualityScore)
		}
	})

	t.Run("clamps lists and fills nil slices", func(t *testing.T) {
		// 7 suggestions (cap 5), 4 refactorHints (cap 3), securityFlags omitted.
		overflow := `{
			"suggestions": ["a","b","c","d","e","f","g"],
			"complexity": "O(1) time, O(1) space",
			"refactorHints": ["r1","r2","r3","r4"],
			"qualityScore": 5
		}`
		svc := newTestService(fakeGen{out: overflow})

		got, err := svc.ReviewCode(context.Background(), "snip3", "python", "code")
		if err != nil {
			t.Fatalf("ReviewCode returned error: %v", err)
		}
		if len(got.Suggestions) != maxSuggestions {
			t.Errorf("len(Suggestions) = %d, want %d", len(got.Suggestions), maxSuggestions)
		}
		if len(got.RefactorHints) != maxRefactorHints {
			t.Errorf("len(RefactorHints) = %d, want %d", len(got.RefactorHints), maxRefactorHints)
		}
		if got.SecurityFlags == nil {
			t.Error("omitted SecurityFlags should normalize to non-nil empty slice")
		}
	})

	t.Run("rejects malformed JSON", func(t *testing.T) {
		svc := newTestService(fakeGen{out: "this is not json"})

		if _, err := svc.ReviewCode(context.Background(), "snip4", "go", "code"); err == nil {
			t.Fatal("expected an error for malformed JSON, got nil")
		}
	})

	t.Run("rejects out-of-range quality score", func(t *testing.T) {
		bad := `{"suggestions":[],"complexity":"O(1)","refactorHints":[],"securityFlags":[],"qualityScore":42}`
		svc := newTestService(fakeGen{out: bad})

		if _, err := svc.ReviewCode(context.Background(), "snip5", "go", "code"); err == nil {
			t.Fatal("expected a validation error for qualityScore=42, got nil")
		}
	})

	t.Run("returns ErrRateLimited when the limiter is exhausted", func(t *testing.T) {
		svc := &AIService{
			gen:     fakeGen{out: validJSON},
			limiter: rate.NewLimiter(0, 0), // zero rate + zero burst => always denies
		}
		if _, err := svc.ReviewCode(context.Background(), "snip6", "go", "code"); !errors.Is(err, ErrRateLimited) {
			t.Fatalf("err = %v, want ErrRateLimited", err)
		}
	})
}
