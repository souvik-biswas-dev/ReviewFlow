package db

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Collection names are centralised so handlers/resolvers never hard-code
// strings (a typo would silently target the wrong collection).
const (
	UsersCollection         = "users"
	SnippetsCollection      = "snippets"
	ReviewsCollection       = "reviews"
	NotificationsCollection = "notifications"
)

// User is a GitHub-authenticated account. The Mongo _id is our internal user
// id (surfaced to the API as a hex string); GitHubID is GitHub's numeric id and
// is the stable key we upsert on (usernames can change, ids don't).
type User struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	GitHubID       int64              `bson:"githubId"`
	GitHubUsername string             `bson:"githubUsername"`
	AvatarURL      string             `bson:"avatarUrl"`
	CreatedAt      time.Time          `bson:"createdAt"`
	UpdatedAt      time.Time          `bson:"updatedAt"`
}

// Snippet is a piece of code submitted for review. PreviousVersion is the
// prior code (optional); when present, the UI offers a diff view.
type Snippet struct {
	ID              primitive.ObjectID `bson:"_id,omitempty"`
	AuthorID        primitive.ObjectID `bson:"authorId"`
	Title           string             `bson:"title"`
	Language        string             `bson:"language"`
	Code            string             `bson:"code"`
	PreviousVersion string             `bson:"previousVersion,omitempty"`
	CreatedAt       time.Time          `bson:"createdAt"`
	UpdatedAt       time.Time          `bson:"updatedAt"`
}

// Review is a human review comment on a snippet. LineNumber is optional (a
// pointer) so a general, non-line-anchored comment is distinguishable from a
// comment on line 0. ParentReviewID, when set, marks this as a reply to a
// top-level review (the resolver enforces max depth = 1).
type Review struct {
	ID             primitive.ObjectID  `bson:"_id,omitempty"`
	SnippetID      primitive.ObjectID  `bson:"snippetId"`
	AuthorID       primitive.ObjectID  `bson:"authorId"`
	Body           string              `bson:"body"`
	LineNumber     *int                `bson:"lineNumber,omitempty"`
	ParentReviewID *primitive.ObjectID `bson:"parentReviewId,omitempty"`
	CreatedAt      time.Time           `bson:"createdAt"`
}

// Notification is dropped into the bell whenever someone reviews YOUR snippet.
type Notification struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    primitive.ObjectID `bson:"userId" json:"-"` // recipient
	SnippetID primitive.ObjectID `bson:"snippetId" json:"snippetId"`
	ReviewID  primitive.ObjectID `bson:"reviewId" json:"reviewId"`
	Read      bool               `bson:"read" json:"read"`
	CreatedAt time.Time          `bson:"createdAt" json:"createdAt"`
}

// EnsureIndexes creates every index the app relies on. It is safe to call on
// each startup: CreateOne is idempotent when the spec (keys + options) matches
// an existing index. Call this once after Connect, before serving traffic.
func (c *Client) EnsureIndexes(ctx context.Context) error {
	// users.githubId UNIQUE: one account per GitHub identity, and a fast lookup
	// for the OAuth upsert.
	if _, err := c.Database.Collection(UsersCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "githubId", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uniq_githubId"),
	}); err != nil {
		return fmt.Errorf("users index: %w", err)
	}

	// snippets {authorId:1, createdAt:-1}: list one author's snippets newest-first.
	if _, err := c.Database.Collection(SnippetsCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "authorId", Value: 1}, {Key: "createdAt", Value: -1}},
		Options: options.Index().SetName("authorId_createdAt"),
	}); err != nil {
		return fmt.Errorf("snippets index: %w", err)
	}

	// reviews {snippetId:1, createdAt:1}: fetch a snippet's reviews in
	// conversation (oldest-first) order.
	if _, err := c.Database.Collection(ReviewsCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "snippetId", Value: 1}, {Key: "createdAt", Value: 1}},
		Options: options.Index().SetName("snippetId_createdAt"),
	}); err != nil {
		return fmt.Errorf("reviews index: %w", err)
	}

	// ai_reviews.snippetId UNIQUE: one AI review per snippet, and the key the
	// AI service upserts on (replacing a prior review on re-run).
	if _, err := c.Database.Collection("ai_reviews").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "snippetId", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uniq_ai_snippetId"),
	}); err != nil {
		return fmt.Errorf("ai_reviews index: %w", err)
	}

	// notifications {userId:1, createdAt:-1}: each user's bell, newest-first.
	if _, err := c.Database.Collection(NotificationsCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("userId_createdAt"),
		},
		{
			Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "read", Value: 1}},
			Options: options.Index().SetName("userId_read"),
		},
	}); err != nil {
		return fmt.Errorf("notifications index: %w", err)
	}

	return nil
}
