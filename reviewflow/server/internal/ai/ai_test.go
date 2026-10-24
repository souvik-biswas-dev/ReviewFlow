package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"reviewflow/internal/domain"
	"reviewflow/internal/ws"
)

const goodJSON = `{"suggestions":["a","b"],"complexity":"O(n) time, O(1) space","refactorHints":["x"],"securityFlags":[],"qualityScore":8}`

type fakeStore struct {
	mu      sync.Mutex
	status  map[string]string
	reasons map[string]string
	saved   map[string]*domain.AIReview
	saves   int
}

func newFakeStore() *fakeStore {
	return &fakeStore{status: map[string]string{}, reasons: map[string]string{}, saved: map[string]*domain.AIReview{}}
}
func (f *fakeStore) MarkAIPending(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[id] = "pending"
	return nil
}
func (f *fakeStore) SaveAIReady(_ context.Context, r *domain.AIReview) (*domain.AIReview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[r.SnippetID] = "ready"
	f.saves++
	cp := *r
	cp.ID = "id-" + r.SnippetID
	f.saved[r.SnippetID] = &cp
	return &cp, nil
}
func (f *fakeStore) MarkAIFailed(_ context.Context, id, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[id] = "failed"
	f.reasons[id] = reason
	return nil
}
func (f *fakeStore) PendingAISnippets(context.Context, int) ([]string, error) { return nil, nil }
func (f *fakeStore) get(id string) (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status[id], f.reasons[id]
}

type fakePub struct {
	mu   sync.Mutex
	msgs []ws.Message
}

func (p *fakePub) BroadcastToRoom(_ string, m ws.Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, m)
}
func (p *fakePub) types() []ws.MessageType {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []ws.MessageType
	for _, m := range p.msgs {
		out = append(out, m.Type)
	}
	return out
}

type genFunc func(ctx context.Context, n int) (string, error)

type fakeGen struct {
	calls atomic.Int32
	fn    genFunc
}

func (g *fakeGen) Generate(ctx context.Context, _, _ string) (string, error) {
	return g.fn(ctx, int(g.calls.Add(1)))
}

func setup(t *testing.T, cfg Config, fn genFunc, lim *rate.Limiter) (*Service, *fakeStore, *fakePub, *fakeGen) {
	t.Helper()
	st, pub, gen := newFakeStore(), &fakePub{}, &fakeGen{fn: fn}
	if lim == nil {
		lim = rate.NewLimiter(rate.Inf, 1)
	}
	svc := newService(cfg, gen, st, pub, lim)
	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)
	t.Cleanup(func() { cancel(); svc.Wait() })
	return svc, st, pub, gen
}

func waitStatus(t *testing.T, st *fakeStore, id, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := st.get(id); s == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	s, r := st.get(id)
	t.Fatalf("snippet %s: want status %s, have %s (%s)", id, want, s, r)
}

func TestHappyPath(t *testing.T) {
	svc, st, pub, _ := setup(t, Config{}, func(context.Context, int) (string, error) { return goodJSON, nil }, nil)
	if err := svc.Enqueue(context.Background(), "s1", "go", "code"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, st, "s1", "ready")
	types := pub.types()
	if len(types) != 2 || types[0] != ws.TypeAIReviewPending || types[1] != ws.TypeAIReviewReady {
		t.Fatalf("frames: %v", types)
	}
	if st.saved["s1"].QualityScore != 8 {
		t.Fatalf("bad saved review: %+v", st.saved["s1"])
	}
}

func TestQuotaRetriesOnceThenSucceeds(t *testing.T) {
	cfg := Config{DefaultRetryDelay: 20 * time.Millisecond, Timeout: 5 * time.Second}
	svc, st, _, gen := setup(t, cfg, func(_ context.Context, n int) (string, error) {
		if n == 1 {
			return "", &RateLimitError{Message: "slow down"}
		}
		return goodJSON, nil
	}, nil)
	_ = svc.Enqueue(context.Background(), "s1", "go", "c")
	waitStatus(t, st, "s1", "ready")
	if gen.calls.Load() != 2 {
		t.Fatalf("want exactly 2 calls, got %d", gen.calls.Load())
	}
}

func TestQuotaTwiceFailsWithoutThirdAttempt(t *testing.T) {
	cfg := Config{DefaultRetryDelay: 10 * time.Millisecond, Timeout: 5 * time.Second}
	svc, st, pub, gen := setup(t, cfg, func(context.Context, int) (string, error) {
		return "", &RateLimitError{Message: "quota"}
	}, nil)
	_ = svc.Enqueue(context.Background(), "s1", "go", "c")
	waitStatus(t, st, "s1", "failed")
	if gen.calls.Load() != 2 {
		t.Fatalf("single-shot retry violated: %d calls", gen.calls.Load())
	}
	if _, reason := st.get("s1"); reason == "" {
		t.Fatal("failure should carry a reason")
	}
	if last := pub.types(); last[len(last)-1] != ws.TypeAIReviewFailed {
		t.Fatalf("frames: %v", last)
	}
}

func TestRetrySkippedWhenItCannotFitTheBudget(t *testing.T) {
	cfg := Config{Timeout: 300 * time.Millisecond}
	svc, st, _, gen := setup(t, cfg, func(context.Context, int) (string, error) {
		return "", &RateLimitError{RetryAfter: 30 * time.Second, Message: "per-minute"}
	}, nil)
	_ = svc.Enqueue(context.Background(), "s1", "go", "c")
	waitStatus(t, st, "s1", "failed")
	if gen.calls.Load() != 1 {
		t.Fatalf("should not wait 30s inside a 300ms budget: %d calls", gen.calls.Load())
	}
}

func TestTimeout(t *testing.T) {
	cfg := Config{Timeout: 80 * time.Millisecond}
	svc, st, _, _ := setup(t, cfg, func(ctx context.Context, _ int) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}, nil)
	_ = svc.Enqueue(context.Background(), "s1", "go", "c")
	waitStatus(t, st, "s1", "failed")
	if _, r := st.get("s1"); r == "" {
		t.Fatal("expected a timeout reason")
	}
}

func TestInvalidJSONFails(t *testing.T) {
	for name, body := range map[string]string{
		"not json":      "I think this code is fine!",
		"score range":   `{"complexity":"O(1)","qualityScore":11}`,
		"no complexity": `{"complexity":"","qualityScore":5}`,
		"fractional":    `{"complexity":"O(1)","qualityScore":7.5}`,
	} {
		t.Run(name, func(t *testing.T) {
			svc, st, _, _ := setup(t, Config{}, func(context.Context, int) (string, error) { return body, nil }, nil)
			_ = svc.Enqueue(context.Background(), "s1", "go", "c")
			waitStatus(t, st, "s1", "failed")
		})
	}
}

func TestParseToleratesFencesAndCaps(t *testing.T) {
	raw := "```json\n" + `{"suggestions":["1","2","3","4","5","6","7"],"complexity":"O(n)","refactorHints":["a","b","c","d"],"securityFlags":"oops","qualityScore":"9"}` + "\n```"
	r, err := ParseReview(raw, "s", "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Suggestions) != 5 || len(r.RefactorHints) != 3 || r.SecurityFlags == nil || len(r.SecurityFlags) != 0 || r.QualityScore != 9 {
		t.Fatalf("bad normalisation: %+v", r)
	}
}

// The old pipeline rejected calls beyond a burst of 1, silently dropping the
// second snippet. Here a burst of snippets must ALL get reviews, spaced out.
func TestBurstIsPacedNotDropped(t *testing.T) {
	lim := rate.NewLimiter(rate.Every(60*time.Millisecond), 1)
	var mu sync.Mutex
	var stamps []time.Time
	svc, st, _, _ := setup(t, Config{Workers: 3}, func(context.Context, int) (string, error) {
		mu.Lock()
		stamps = append(stamps, time.Now())
		mu.Unlock()
		return goodJSON, nil
	}, lim)
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := svc.Enqueue(context.Background(), id, "go", "c"); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		waitStatus(t, st, id, "ready")
	}
	mu.Lock()
	defer mu.Unlock()
	if span := stamps[len(stamps)-1].Sub(stamps[0]); span < 150*time.Millisecond {
		t.Fatalf("4 calls at 1/60ms should span >=180ms, spanned %s", span)
	}
}

func TestEnqueueIsIdempotentWhileInflight(t *testing.T) {
	release := make(chan struct{})
	svc, st, _, gen := setup(t, Config{}, func(context.Context, int) (string, error) {
		<-release
		return goodJSON, nil
	}, nil)
	for i := 0; i < 5; i++ {
		_ = svc.Enqueue(context.Background(), "s1", "go", "c")
	}
	close(release)
	waitStatus(t, st, "s1", "ready")
	if gen.calls.Load() != 1 || st.saves != 1 {
		t.Fatalf("duplicates ran: calls=%d saves=%d", gen.calls.Load(), st.saves)
	}
}

func TestQueueFull(t *testing.T) {
	st, pub := newFakeStore(), &fakePub{}
	svc := newService(Config{QueueSize: 1}, &fakeGen{fn: func(context.Context, int) (string, error) { return goodJSON, nil }}, st, pub, rate.NewLimiter(rate.Inf, 1))
	// workers not started: queue holds 1
	if err := svc.Enqueue(context.Background(), "a", "go", "c"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Enqueue(context.Background(), "b", "go", "c"); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("want ErrQueueFull, got %v", err)
	}
	if s, _ := st.get("b"); s != "failed" {
		t.Fatalf("overflow should be recorded as failed, got %s", s)
	}
}

// --- Gemini REST client -----------------------------------------------------

func TestGeminiClientRequestShapeAndParsing(t *testing.T) {
	var gotPath, gotKey string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.Path, r.Header.Get("x-goog-api-key")
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hidden","thought":true},{"text":"{\"ok\":true}"}]}}]}`))
	}))
	defer srv.Close()

	c := NewGeminiClient("KEY", "gemini-3.5-flash", srv.URL)
	out, err := c.Generate(context.Background(), "SYS", "PROMPT")
	if err != nil || out != `{"ok":true}` {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if gotPath != "/v1beta/models/gemini-3.5-flash:generateContent" || gotKey != "KEY" {
		t.Fatalf("path=%s key=%s", gotPath, gotKey)
	}
	gc := body["generationConfig"].(map[string]any)
	if gc["responseMimeType"] != "application/json" {
		t.Fatalf("JSON mode not requested: %v", gc)
	}
	if body["systemInstruction"] == nil {
		t.Fatal("system instruction missing")
	}
}

func TestGeminiClient429ParsesRetryInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"7s"}]}}`))
	}))
	defer srv.Close()
	_, err := NewGeminiClient("k", "m", srv.URL).Generate(context.Background(), "s", "p")
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second {
		t.Fatalf("want RateLimitError{7s}, got %#v", err)
	}
}

func TestGeminiClientOtherErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"model no longer available","status":"NOT_FOUND"}}`))
	}))
	defer srv.Close()
	_, err := NewGeminiClient("k", "old-model", srv.URL).Generate(context.Background(), "s", "p")
	var api *APIError
	if !errors.As(err, &api) || api.Status != 404 {
		t.Fatalf("want APIError 404, got %v", err)
	}
	if msg := userMessage(err, 30*time.Second); msg == "" || !contains(msg, "GEMINI_MODEL") {
		t.Fatalf("404 should point at GEMINI_MODEL: %q", msg)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
