// Package domain holds the API-facing models. The same structs back the
// GraphQL schema (gqlgen autobinds to them) and the JSON payloads that travel
// over WebSocket frames, so a review looks identical no matter which path
// delivered it to the browser.
package domain

import "time"

// AI review lifecycle, as exposed in Snippet.aiStatus.
const (
	AIStatusDisabled = "disabled" // server has no Gemini key
	AIStatusNone     = "none"     // AI is enabled but never ran for this snippet
	AIStatusPending  = "pending"  // queued or running
	AIStatusReady    = "ready"    // aiReview is populated
	AIStatusFailed   = "failed"   // aiError says why; can be retried
)

type User struct {
	ID             string    `json:"id"`
	GithubUsername string    `json:"githubUsername"`
	AvatarURL      string    `json:"avatarUrl"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Review struct {
	ID             string    `json:"id"`
	SnippetID      string    `json:"snippetId"`
	Author         *User     `json:"author"`
	Body           string    `json:"body"`
	LineNumber     *int      `json:"lineNumber"`
	ParentReviewID *string   `json:"parentReviewId"`
	CreatedAt      time.Time `json:"createdAt"`
}

type AIReview struct {
	ID            string    `json:"id"`
	SnippetID     string    `json:"snippetId"`
	Suggestions   []string  `json:"suggestions"`
	Complexity    string    `json:"complexity"`
	RefactorHints []string  `json:"refactorHints"`
	SecurityFlags []string  `json:"securityFlags"`
	QualityScore  int       `json:"qualityScore"`
	Language      string    `json:"language"`
	GeneratedAt   time.Time `json:"generatedAt"`
}

type Snippet struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Language        string    `json:"language"`
	Code            string    `json:"code"`
	PreviousVersion *string   `json:"previousVersion"`
	Author          *User     `json:"author"`
	Reviews         []*Review `json:"reviews"`
	AIReview        *AIReview `json:"aiReview"`
	AIStatus        string    `json:"aiStatus"`
	AIError         *string   `json:"aiError"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Notification is one row of the bell dropdown (REST, not GraphQL).
type Notification struct {
	ID           string    `json:"id"`
	SnippetID    string    `json:"snippetId"`
	SnippetTitle string    `json:"snippetTitle"`
	ReviewID     string    `json:"reviewId"`
	Read         bool      `json:"read"`
	CreatedAt    time.Time `json:"createdAt"`
}
