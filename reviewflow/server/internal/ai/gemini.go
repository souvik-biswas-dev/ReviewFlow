package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Generator is the seam tests replace; production uses GeminiClient.
type Generator interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// RateLimitError means Gemini answered 429 / RESOURCE_EXHAUSTED.
type RateLimitError struct {
	RetryAfter time.Duration // from the API's RetryInfo, 0 if absent
	Message    string
}

func (e *RateLimitError) Error() string { return "gemini quota exhausted: " + e.Message }

// APIError is any other non-2xx answer.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gemini API error (HTTP %d): %s", e.Status, e.Message)
}

// GeminiClient calls the Gemini REST API directly (generateContent). Using
// plain net/http keeps the dependency surface small and the 429 handling exact.
type GeminiClient struct {
	APIKey  string
	Model   string
	BaseURL string
	HTTP    *http.Client
}

func NewGeminiClient(apiKey, model, baseURL string) *GeminiClient {
	return &GeminiClient{APIKey: apiKey, Model: model, BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{}}
}

type part struct {
	Text    string `json:"text,omitempty"`
	Thought bool   `json:"thought,omitempty"`
}
type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}
type generateRequest struct {
	SystemInstruction *content         `json:"systemInstruction,omitempty"`
	Contents          []content        `json:"contents"`
	GenerationConfig  generationConfig `json:"generationConfig"`
}

// Temperature is intentionally left at the model default: Google recommends
// that for Gemini 3.x, and forcing a low value can degrade output there.
type generationConfig struct {
	ResponseMimeType string `json:"responseMimeType"`
}
type generateResponse struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}
type errorEnvelope struct {
	Error struct {
		Code    int               `json:"code"`
		Message string            `json:"message"`
		Status  string            `json:"status"`
		Details []json.RawMessage `json:"details"`
	} `json:"error"`
}

func (g *GeminiClient) Generate(ctx context.Context, system, prompt string) (string, error) {
	body, err := json.Marshal(generateRequest{
		SystemInstruction: &content{Parts: []part{{Text: system}}},
		Contents:          []content{{Role: "user", Parts: []part{{Text: prompt}}}},
		GenerationConfig:  generationConfig{ResponseMimeType: "application/json"},
	})
	if err != nil {
		return "", err
	}
	model := strings.TrimPrefix(g.Model, "models/")
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", g.BaseURL, url.PathEscape(model))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.APIKey)

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", classifyError(resp.StatusCode, raw)
	}

	var out generateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode gemini response: %w", err)
	}
	if out.PromptFeedback.BlockReason != "" {
		return "", fmt.Errorf("gemini blocked the prompt: %s", out.PromptFeedback.BlockReason)
	}
	if len(out.Candidates) == 0 {
		return "", errors.New("gemini returned no candidates")
	}
	var sb strings.Builder
	for _, p := range out.Candidates[0].Content.Parts {
		if !p.Thought {
			sb.WriteString(p.Text)
		}
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return "", fmt.Errorf("gemini returned an empty response (finishReason=%s)", out.Candidates[0].FinishReason)
	}
	return text, nil
}

func classifyError(status int, raw []byte) error {
	var env errorEnvelope
	_ = json.Unmarshal(raw, &env)
	msg := env.Error.Message
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
	}
	if status == http.StatusTooManyRequests || env.Error.Status == "RESOURCE_EXHAUSTED" {
		return &RateLimitError{RetryAfter: retryDelay(env.Error.Details), Message: msg}
	}
	return &APIError{Status: status, Message: msg}
}

// retryDelay extracts google.rpc.RetryInfo.retryDelay ("33s") if present.
func retryDelay(details []json.RawMessage) time.Duration {
	for _, d := range details {
		var info struct {
			Type  string `json:"@type"`
			Delay string `json:"retryDelay"`
		}
		if json.Unmarshal(d, &info) == nil && strings.HasSuffix(info.Type, "RetryInfo") && info.Delay != "" {
			if dur, err := time.ParseDuration(info.Delay); err == nil && dur > 0 {
				return dur
			}
		}
	}
	return 0
}
