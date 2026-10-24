package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"reviewflow/internal/ai"
	"reviewflow/internal/api"
	"reviewflow/internal/config"
	"reviewflow/internal/store"
	"reviewflow/internal/ws"
)

// Run with: MONGO_TEST_URI=mongodb://localhost:27017 go test ./internal/api
const goodReview = `{"suggestions":["Use a map"],"complexity":"O(n) time, O(n) space","refactorHints":["Extract helper"],"securityFlags":[],"qualityScore":7}`

type env struct {
	t      *testing.T
	srv    *httptest.Server
	st     *store.Store
	gemini *httptest.Server
	mode   atomic.Value // "ok" | "quota-once" | "404"
	calls  atomic.Int32
	delay  atomic.Int64 // simulated model latency, ms
}

func setup(t *testing.T) *env {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set")
	}
	e := &env{t: t}
	e.mode.Store("ok")
	e.gemini = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := e.calls.Add(1)
		time.Sleep(time.Duration(e.delay.Load()) * time.Millisecond)
		switch e.mode.Load().(string) {
		case "404":
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"code":404,"message":"This model is no longer available","status":"NOT_FOUND"}}`)
		case "quota-once":
			if n == 1 {
				w.WriteHeader(429)
				fmt.Fprint(w, `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"1s"}]}}`)
				return
			}
			fallthrough
		default:
			out, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": goodReview}}}}}})
			w.Write(out)
		}
	}))

	dbName := fmt.Sprintf("rf_e2e_%d", time.Now().UnixNano())
	st, err := store.Connect(context.Background(), uri, dbName)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureIndexes(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Port: "0", Environment: "development", JWTSecret: "test-secret-test-secret-test-secret",
		FrontendURL: "http://localhost:5173", CORSOrigins: []string{"http://localhost:5173"},
		AllowGuestLogin: true, GeminiAPIKey: "k", GeminiModel: "m", GeminiBaseURL: e.gemini.URL,
	}
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	svc := ai.NewService(ai.Config{RequestsPerMinute: 600, Timeout: 10 * time.Second}, ai.NewGeminiClient("k", "m", e.gemini.URL), st, hub)
	svc.Start(ctx)
	e.st = st
	e.srv = httptest.NewServer(api.NewRouter(api.Deps{Config: cfg, Store: st, Hub: hub, AI: svc}))

	t.Cleanup(func() {
		e.srv.Close()
		cancel()
		<-hub.Done()
		svc.Wait()
		e.gemini.Close()
		_ = st.Database().Drop(context.Background())
		_ = st.Close(context.Background())
	})
	return e
}

func (e *env) guest(name string) (token, id string) {
	e.t.Helper()
	res, err := http.Post(e.srv.URL+"/auth/guest", "application/json", strings.NewReader(fmt.Sprintf(`{"username":%q}`, name)))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		}
	}
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&out) != nil || out.Token == "" {
		e.t.Fatalf("guest login %s failed: %d", name, res.StatusCode)
	}
	return out.Token, out.User.ID
}

type gqlResp struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct{ Message string }
}

func (e *env) gql(token, query string, vars map[string]any) gqlResp {
	e.t.Helper()
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, _ := http.NewRequest("POST", e.srv.URL+"/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out gqlResp
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *env) dial(token, snippetID string) (*websocket.Conn, *http.Response, error) {
	url := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/ws/" + snippetID + "?token=" + token
	return websocket.DefaultDialer.Dial(url, http.Header{"Origin": {"http://localhost:5173"}})
}

// next reads frames until one of the wanted type arrives.
func next(t *testing.T, c *websocket.Conn, want ws.MessageType) ws.Message {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(8 * time.Second))
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %s: %v", want, err)
		}
		var m struct {
			Type           ws.MessageType  `json:"type"`
			SnippetID      string          `json:"snippetId"`
			Payload        json.RawMessage `json:"payload"`
			SenderUsername string          `json:"senderUsername"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if m.Type == want {
			return ws.Message{Type: m.Type, SnippetID: m.SnippetID, Payload: m.Payload, SenderUsername: m.SenderUsername}
		}
	}
}

const createQ = `mutation($i: CreateSnippetInput!){ createSnippet(input:$i){ id title aiStatus author{githubUsername} } }`
const snippetQ = `query($id: ID!){ snippet(id:$id){ id aiStatus aiError aiReview{qualityScore complexity securityFlags} reviews{ id body lineNumber parentReviewId author{githubUsername} } } }`
const reviewQ = `mutation($s: ID!, $i: AddReviewInput!){ addReview(snippetId:$s, input:$i){ id body parentReviewId author{githubUsername} } }`

func waitAI(t *testing.T, e *env, token, id, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var d struct{ Snippet map[string]any }
		_ = json.Unmarshal(e.gql(token, snippetQ, map[string]any{"id": id}).Data, &d)
		if d.Snippet["aiStatus"] == want {
			return d.Snippet
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("snippet never reached aiStatus=%s", want)
	return nil
}

func TestEndToEnd(t *testing.T) {
	e := setup(t)
	e.delay.Store(600) // like a real model, slower than a socket handshake
	aliceTok, aliceID := e.guest("alice")
	bobTok, _ := e.guest("bob")

	// Auth sanity.
	if tok2, id2 := e.guest("ALICE"); id2 != aliceID || tok2 == "" {
		t.Fatalf("guest names are case-insensitive identities: %s vs %s", id2, aliceID)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+aliceTok)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 200 {
		t.Fatalf("/auth/me: %d", res.StatusCode)
	}

	// Anonymous writes are refused.
	if r := e.gql("", createQ, map[string]any{"i": map[string]any{"title": "x", "language": "go", "code": "x"}}); len(r.Errors) == 0 {
		t.Fatal("anonymous createSnippet should fail")
	}

	// 1. alice creates a snippet; response is immediate with AI pending.
	r := e.gql(aliceTok, createQ, map[string]any{"i": map[string]any{
		"title": "Two-sum", "language": "go", "code": "func f() {}", "previousVersion": "func old() {}"}})
	if len(r.Errors) > 0 {
		t.Fatalf("createSnippet: %v", r.Errors)
	}
	var created struct{ CreateSnippet struct{ ID, AIStatus string } }
	_ = json.Unmarshal(r.Data, &created)
	sid := created.CreateSnippet.ID
	if sid == "" || created.CreateSnippet.AIStatus != "pending" {
		t.Fatalf("want pending snippet, got %+v", created)
	}

	// 2. Both users join the room; presence works.
	aliceWS, _, err := e.dial(aliceTok, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer aliceWS.Close()
	next(t, aliceWS, ws.TypePresenceList)
	bobWS, _, err := e.dial(bobTok, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer bobWS.Close()
	if list := next(t, bobWS, ws.TypePresenceList); !strings.Contains(string(list.Payload.(json.RawMessage)), "alice") {
		t.Fatalf("bob should see alice: %s", list.Payload)
	}
	if j := next(t, aliceWS, ws.TypePresenceJoin); j.SenderUsername != "bob" {
		t.Fatalf("alice should see bob join: %+v", j)
	}

	// 3. The AI review lands: pushed over the socket AND persisted.
	ready := next(t, bobWS, ws.TypeAIReviewReady)
	if !strings.Contains(string(ready.Payload.(json.RawMessage)), `"qualityScore":7`) {
		t.Fatalf("bad ai payload: %s", ready.Payload)
	}
	snap := waitAI(t, e, bobTok, sid, "ready")
	if snap["aiReview"] == nil {
		t.Fatal("aiReview missing after ready")
	}

	// 4. bob reviews alice's snippet: DB + live frame to everyone + notification.
	r = e.gql(bobTok, reviewQ, map[string]any{"s": sid, "i": map[string]any{"body": "Looks risky", "lineNumber": 3}})
	if len(r.Errors) > 0 {
		t.Fatalf("addReview: %v", r.Errors)
	}
	var added struct{ AddReview struct{ ID string } }
	_ = json.Unmarshal(r.Data, &added)
	for name, c := range map[string]*websocket.Conn{"alice": aliceWS, "bob": bobWS} {
		m := next(t, c, ws.TypeReviewAdded)
		if !strings.Contains(string(m.Payload.(json.RawMessage)), added.AddReview.ID) {
			t.Fatalf("%s got wrong review frame: %s", name, m.Payload)
		}
	}
	notif := func(tok string) (int, int) {
		req, _ := http.NewRequest("GET", e.srv.URL+"/notifications", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("notifications: %v %v", err, res)
		}
		defer res.Body.Close()
		var out struct {
			Notifications []struct{ SnippetTitle string }
			UnreadCount   int
		}
		_ = json.NewDecoder(res.Body).Decode(&out)
		return len(out.Notifications), out.UnreadCount
	}
	if n, u := notif(aliceTok); n != 1 || u != 1 {
		t.Fatalf("alice should have 1 unread notification, got %d/%d", n, u)
	}
	if n, _ := notif(bobTok); n != 0 {
		t.Fatalf("reviewers are not notified of their own review")
	}

	// 5. Replies flatten to depth 1; self-review does not notify.
	reply1 := e.gql(aliceTok, reviewQ, map[string]any{"s": sid, "i": map[string]any{"body": "Why?", "parentReviewId": added.AddReview.ID}})
	var rp struct {
		AddReview struct{ ID, ParentReviewID string }
	}
	_ = json.Unmarshal(reply1.Data, &rp)
	reply2 := e.gql(bobTok, reviewQ, map[string]any{"s": sid, "i": map[string]any{"body": "Because", "parentReviewId": rp.AddReview.ID}})
	var rp2 struct {
		AddReview struct{ ParentReviewID string }
	}
	_ = json.Unmarshal(reply2.Data, &rp2)
	if rp.AddReview.ParentReviewID != added.AddReview.ID || rp2.AddReview.ParentReviewID != added.AddReview.ID {
		t.Fatalf("threads must stay 1 level deep: %v / %v", rp, rp2)
	}
	if _, u := notif(aliceTok); u != 2 { // bob's reply counts, alice's own does not
		t.Fatalf("want 2 unread for alice, got %d", u)
	}
	req, _ = http.NewRequest("POST", e.srv.URL+"/notifications/read", nil)
	req.Header.Set("Authorization", "Bearer "+aliceTok)
	_, _ = http.DefaultClient.Do(req)
	if _, u := notif(aliceTok); u != 0 {
		t.Fatalf("mark-read failed: %d", u)
	}

	// 6. Typing relays to others, with the sender stamped by the server.
	_ = bobWS.WriteMessage(websocket.TextMessage, []byte(`{"type":"typing","payload":{"line":3},"senderUsername":"mallory"}`))
	if m := next(t, aliceWS, ws.TypeTyping); m.SenderUsername != "bob" {
		t.Fatalf("sender must be server-stamped: %+v", m)
	}
	_ = aliceWS.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`))
	next(t, aliceWS, ws.TypePong)

	// 7. Leaving is announced.
	bobWS.Close()
	next(t, aliceWS, ws.TypePresenceLeave)

	// 8. The dashboard list returns everything it needs in one query.
	r = e.gql(aliceTok, `{ snippets(limit: 10){ id title author{githubUsername avatarUrl} reviews{id} aiReview{id} } }`, nil)
	var list struct {
		Snippets []struct {
			Reviews  []struct{ ID string }
			AIReview *struct{ ID string }
		}
	}
	_ = json.Unmarshal(r.Data, &list)
	if len(list.Snippets) != 1 || len(list.Snippets[0].Reviews) != 3 || list.Snippets[0].AIReview == nil {
		t.Fatalf("dashboard list wrong: %s %v", r.Data, r.Errors)
	}
}

func TestSocketGuards(t *testing.T) {
	e := setup(t)
	tok, _ := e.guest("alice")
	r := e.gql(tok, createQ, map[string]any{"i": map[string]any{"title": "t", "language": "go", "code": "x"}})
	var c struct{ CreateSnippet struct{ ID string } }
	_ = json.Unmarshal(r.Data, &c)
	base := "ws" + strings.TrimPrefix(e.srv.URL, "http")

	cases := []struct {
		name   string
		url    string
		origin string
		want   int
	}{
		{"no token", base + "/ws/" + c.CreateSnippet.ID, "http://localhost:5173", 401},
		{"bad token", base + "/ws/" + c.CreateSnippet.ID + "?token=garbage", "http://localhost:5173", 401},
		{"unknown snippet", base + "/ws/000000000000000000000000?token=" + tok, "http://localhost:5173", 404},
		{"junk snippet id", base + "/ws/not-an-id?token=" + tok, "http://localhost:5173", 404},
		{"foreign origin", base + "/ws/" + c.CreateSnippet.ID + "?token=" + tok, "http://evil.example", 403},
	}
	for _, tc := range cases {
		_, res, err := websocket.DefaultDialer.Dial(tc.url, http.Header{"Origin": {tc.origin}})
		if err == nil || res == nil || res.StatusCode != tc.want {
			t.Errorf("%s: want HTTP %d, got %v (%v)", tc.name, tc.want, res, err)
		}
	}
}

func TestAIFailureIsVisibleAndRetryable(t *testing.T) {
	e := setup(t)
	e.mode.Store("404") // the retired-model scenario
	aTok, _ := e.guest("alice")
	bTok, _ := e.guest("bob")
	r := e.gql(aTok, createQ, map[string]any{"i": map[string]any{"title": "t", "language": "go", "code": "x"}})
	var c struct{ CreateSnippet struct{ ID string } }
	_ = json.Unmarshal(r.Data, &c)
	sid := c.CreateSnippet.ID

	snap := waitAI(t, e, aTok, sid, "failed")
	if msg, _ := snap["aiError"].(string); !strings.Contains(msg, "GEMINI_MODEL") {
		t.Fatalf("failure should tell the user what to fix: %q", msg)
	}

	const retryQ = `mutation($s: ID!){ retryAIReview(snippetId:$s){ aiStatus } }`
	if r := e.gql(bTok, retryQ, map[string]any{"s": sid}); len(r.Errors) == 0 {
		t.Fatal("only the author may retry")
	}
	e.mode.Store("ok") // model fixed
	if r := e.gql(aTok, retryQ, map[string]any{"s": sid}); len(r.Errors) > 0 {
		t.Fatalf("retry: %v", r.Errors)
	}
	waitAI(t, e, aTok, sid, "ready")
}

func TestQuotaRetryEndToEnd(t *testing.T) {
	e := setup(t)
	e.mode.Store("quota-once")
	tok, _ := e.guest("alice")
	r := e.gql(tok, createQ, map[string]any{"i": map[string]any{"title": "t", "language": "go", "code": "x"}})
	var c struct{ CreateSnippet struct{ ID string } }
	_ = json.Unmarshal(r.Data, &c)
	waitAI(t, e, tok, c.CreateSnippet.ID, "ready")
	if e.calls.Load() != 2 {
		t.Fatalf("want 1 quota failure + 1 retry = 2 Gemini calls, got %d", e.calls.Load())
	}
}

func TestLogoutClearsCookieAndConfigEndpoint(t *testing.T) {
	e := setup(t)
	res, _ := http.Post(e.srv.URL+"/auth/logout", "application/json", nil)
	var cleared bool
	for _, c := range res.Cookies() {
		if c.Name == "rf_token" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout must expire the session cookie")
	}
	res, _ = http.Get(e.srv.URL + "/auth/config")
	var cfg map[string]bool
	_ = json.NewDecoder(res.Body).Decode(&cfg)
	if !cfg["guest"] || cfg["github"] || !cfg["ai"] {
		t.Fatalf("config: %v", cfg)
	}
	if res, _ := http.Get(e.srv.URL + "/auth/github"); res.StatusCode != 503 {
		t.Fatalf("unconfigured github should 503, got %d", res.StatusCode)
	}
	if res, _ := http.Get(e.srv.URL + "/health"); res.StatusCode != 200 {
		t.Fatalf("health: %d", res.StatusCode)
	}
}
