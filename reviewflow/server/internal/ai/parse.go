package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"reviewflow/internal/domain"
)

const (
	MaxSuggestions   = 5
	MaxRefactorHints = 3
)

// ParseReview turns the model's raw output into a validated AIReview. It strips
// code fences defensively and enforces the invariants the UI relies on.
func ParseReview(raw, snippetID, language string) (*domain.AIReview, error) {
	dec := json.NewDecoder(strings.NewReader(stripFences(raw)))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("model returned invalid JSON: %w", err)
	}
	if m == nil {
		return nil, errors.New("model returned invalid JSON: expected an object")
	}

	score, err := toInt(m["qualityScore"])
	if err != nil || score < 1 || score > 10 {
		return nil, fmt.Errorf("qualityScore %v is not an integer in 1-10", m["qualityScore"])
	}
	complexity, _ := m["complexity"].(string)
	if strings.TrimSpace(complexity) == "" {
		return nil, errors.New("complexity is empty")
	}
	return &domain.AIReview{
		SnippetID:     snippetID,
		Suggestions:   strs(m["suggestions"], MaxSuggestions),
		Complexity:    strings.TrimSpace(complexity),
		RefactorHints: strs(m["refactorHints"], MaxRefactorHints),
		SecurityFlags: strs(m["securityFlags"], math.MaxInt32),
		QualityScore:  score,
		Language:      language,
		GeneratedAt:   time.Now().UTC(),
	}, nil
}

// stripFences removes a leading ```json ... ``` wrapper if present.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func toInt(v any) (int, error) {
	var f float64
	switch n := v.(type) {
	case json.Number:
		x, err := n.Float64()
		if err != nil {
			return 0, err
		}
		f = x
	case string:
		x, err := json.Number(strings.TrimSpace(n)).Float64()
		if err != nil {
			return 0, err
		}
		f = x
	default:
		return 0, errors.New("not a number")
	}
	if f != math.Trunc(f) {
		return 0, errors.New("not an integer")
	}
	return int(f), nil
}

// strs returns at most max non-empty strings; anything that is not a list of
// strings becomes an empty list (so GraphQL's non-null lists are satisfied).
func strs(v any, max int) []string {
	out := []string{}
	arr, ok := v.([]any)
	if !ok {
		return out
	}
	for _, e := range arr {
		if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
			if len(out) == max {
				break
			}
		}
	}
	return out
}
