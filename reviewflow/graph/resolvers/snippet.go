// Package resolvers holds the hand-written business logic behind the GraphQL
// resolvers. The generated gqlgen methods (graph/schema.resolvers.go) stay thin
// and delegate here, which keeps generated and hand-written code separate.
package resolvers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"reviewflow/graph/model"
	"reviewflow/internal/ai"
	"reviewflow/internal/auth"
	"reviewflow/internal/db"
	"reviewflow/internal/ws"
)

// errUnauthenticated is returned when a mutation requires a logged-in user.
var errUnauthenticated = errors.New("authentication required")

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

// CreateSnippet persists a new snippet authored by the current user, kicks off
// an asynchronous AI review, and returns the snippet immediately — the frontend
// learns the AI review is ready later via the WebSocket "ai_review_ready" event.
func CreateSnippet(ctx context.Context, database *db.Client, aiSvc *ai.AIService, input model.CreateSnippetInput) (*model.Snippet, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errUnauthenticated
	}
	authorID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errUnauthenticated
	}

	if strings.TrimSpace(input.Title) == "" ||
		strings.TrimSpace(input.Language) == "" ||
		strings.TrimSpace(input.Code) == "" {
		return nil, errors.New("title, language and code are required")
	}

	now := time.Now().UTC()
	snip := db.Snippet{
		AuthorID:  authorID,
		Title:     input.Title,
		Language:  input.Language,
		Code:      input.Code,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if input.PreviousVersion != nil {
		snip.PreviousVersion = *input.PreviousVersion
	}

	// 1. Save synchronously.
	res, err := database.Database.Collection(db.SnippetsCollection).InsertOne(ctx, snip)
	if err != nil {
		return nil, fmt.Errorf("insert snippet: %w", err)
	}
	snip.ID = res.InsertedID.(primitive.ObjectID)

	// 2. Load the author (the schema's `author` field is non-null).
	author, err := loadUser(ctx, database, authorID)
	if err != nil {
		return nil, err
	}

	// 3. Fire-and-forget the AI review. Detached context: the request ctx is
	//    cancelled the moment this mutation returns. ReviewCode applies its own
	//    30s timeout internally.
	if aiSvc != nil {
		go func(snippetID, language, code string) {
			if _, err := aiSvc.ReviewCode(context.Background(), snippetID, language, code); err != nil {
				log.Printf("ai: review for snippet %s failed: %v", snippetID, err)
			}
		}(snip.ID.Hex(), snip.Language, snip.Code)
	}

	return snippetToModel(&snip, author, []*model.Review{}), nil
}

// AddReview inserts a human review, broadcasts it to the snippet's room over
// the WS hub, and drops a notification in the snippet author's bell.
//
// Threading: if input.ParentReviewID names a reply, we flatten to 1 level —
// replies always attach to a top-level review, never to another reply.
func AddReview(ctx context.Context, database *db.Client, hub *ws.Hub, snippetID string, input model.AddReviewInput) (*model.Review, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, errUnauthenticated
	}
	authorID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errUnauthenticated
	}

	if strings.TrimSpace(input.Body) == "" {
		return nil, errors.New("review body is required")
	}

	sOID, err := primitive.ObjectIDFromHex(snippetID)
	if err != nil {
		return nil, errors.New("invalid snippet id")
	}

	// Load the snippet — needed for the notification (snippet author).
	var snip db.Snippet
	if err := database.Database.Collection(db.SnippetsCollection).
		FindOne(ctx, bson.M{"_id": sOID}).Decode(&snip); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errors.New("snippet not found")
		}
		return nil, fmt.Errorf("load snippet: %w", err)
	}

	// Resolve the parent (enforces max thread depth of 1).
	parent, err := resolveParent(ctx, database, sOID, input.ParentReviewID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	rev := db.Review{
		SnippetID:      sOID,
		AuthorID:       authorID,
		Body:           strings.TrimSpace(input.Body),
		LineNumber:     input.LineNumber,
		ParentReviewID: parent,
		CreatedAt:      now,
	}
	res, err := database.Database.Collection(db.ReviewsCollection).InsertOne(ctx, rev)
	if err != nil {
		return nil, fmt.Errorf("insert review: %w", err)
	}
	rev.ID = res.InsertedID.(primitive.ObjectID)

	author, err := loadUser(ctx, database, authorID)
	if err != nil {
		return nil, err
	}
	rm := reviewToModel(&rev, author)

	// Broadcast to everyone in the snippet's WS room. The reviewer's own client
	// also receives this and de-duplicates by id against the optimistic add.
	if hub != nil {
		if msg, err := ws.NewMessage(ws.MessageReviewAdded, snippetID, rm); err == nil {
			hub.BroadcastToRoom(snippetID, msg)
		}
	}

	// Notify the snippet's author — but only when someone else is reviewing.
	if snip.AuthorID != authorID {
		notif := db.Notification{
			UserID:    snip.AuthorID,
			SnippetID: sOID,
			ReviewID:  rev.ID,
			Read:      false,
			CreatedAt: now,
		}
		if _, err := database.Database.Collection(db.NotificationsCollection).
			InsertOne(ctx, notif); err != nil {
			// Best-effort: don't fail the mutation if the bell fails.
			log.Printf("notifications: insert failed: %v", err)
		}
	}

	return rm, nil
}

// resolveParent validates a parent review and flattens nested replies up one
// level so the thread never exceeds depth 1.
func resolveParent(ctx context.Context, database *db.Client, snippetOID primitive.ObjectID, parentID *string) (*primitive.ObjectID, error) {
	if parentID == nil || *parentID == "" {
		return nil, nil
	}
	pOID, err := primitive.ObjectIDFromHex(*parentID)
	if err != nil {
		return nil, errors.New("invalid parentReviewId")
	}

	var parent db.Review
	if err := database.Database.Collection(db.ReviewsCollection).
		FindOne(ctx, bson.M{"_id": pOID, "snippetId": snippetOID}).Decode(&parent); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errors.New("parent review not found for this snippet")
		}
		return nil, fmt.Errorf("load parent review: %w", err)
	}
	// If the "parent" is itself a reply, attach to its parent instead.
	if parent.ParentReviewID != nil {
		return parent.ParentReviewID, nil
	}
	return &pOID, nil
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

// Me returns the currently authenticated user, or (nil, nil) when anonymous.
func Me(ctx context.Context, database *db.Client) (*model.User, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, nil
	}
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, nil
	}
	return loadUser(ctx, database, oid)
}

// GetSnippet loads one snippet with its author + the full flat list of reviews
// (each with its author). Returns (nil, nil) on not-found → GraphQL null.
func GetSnippet(ctx context.Context, database *db.Client, id string) (*model.Snippet, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, nil
	}
	var s db.Snippet
	err = database.Database.Collection(db.SnippetsCollection).
		FindOne(ctx, bson.M{"_id": oid}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load snippet: %w", err)
	}

	author, err := loadUser(ctx, database, s.AuthorID)
	if err != nil {
		return nil, err
	}

	reviews, err := loadReviewsForSnippet(ctx, database, s.ID)
	if err != nil {
		return nil, err
	}

	return snippetToModel(&s, author, reviews), nil
}

// ListSnippets backs the dashboard. It batch-loads authors and review-id lists
// in two queries rather than per-snippet (no N+1).
func ListSnippets(ctx context.Context, database *db.Client, authorID *string, language *string, limit *int) ([]*model.Snippet, error) {
	filter := bson.M{}
	if authorID != nil && *authorID != "" {
		if oid, err := primitive.ObjectIDFromHex(*authorID); err == nil {
			filter["authorId"] = oid
		}
	}
	if language != nil && *language != "" {
		filter["language"] = *language
	}

	lim := int64(20)
	if limit != nil {
		lim = clamp64(int64(*limit), 1, 100)
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(lim)

	cur, err := database.Database.Collection(db.SnippetsCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find snippets: %w", err)
	}
	defer cur.Close(ctx)

	var rows []db.Snippet
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode snippets: %w", err)
	}
	if len(rows) == 0 {
		return []*model.Snippet{}, nil
	}

	// Batch-load all authors in one query.
	authorIDs := make([]primitive.ObjectID, 0, len(rows))
	snippetIDs := make([]primitive.ObjectID, 0, len(rows))
	for i := range rows {
		authorIDs = append(authorIDs, rows[i].AuthorID)
		snippetIDs = append(snippetIDs, rows[i].ID)
	}
	authors, err := loadUsers(ctx, database, authorIDs)
	if err != nil {
		return nil, err
	}

	// Batch-load review ids per snippet (the dashboard only needs counts).
	reviewIDs, err := loadReviewIDsBySnippet(ctx, database, snippetIDs)
	if err != nil {
		return nil, err
	}

	out := make([]*model.Snippet, 0, len(rows))
	for i := range rows {
		s := &rows[i]
		author := authors[s.AuthorID]
		if author == nil {
			author = &model.User{ID: s.AuthorID.Hex(), GithubUsername: "unknown"}
		}
		out = append(out, snippetToModel(s, author, reviewIDs[s.ID]))
	}
	return out, nil
}

// AIReviewForSnippet backs the Snippet.aiReview field resolver. It returns the
// stored review, or (nil, nil) — GraphQL null — when the AI pass hasn't finished
// yet, which the frontend renders as "AI analyzing...".
func AIReviewForSnippet(ctx context.Context, database *db.Client, snippetID string) (*model.AIReview, error) {
	var r ai.AIReview
	err := database.Database.Collection(ai.AIReviewsCollection).
		FindOne(ctx, bson.M{"snippetId": snippetID}).Decode(&r)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load ai review: %w", err)
	}
	return aiReviewToModel(&r), nil
}

// ---------------------------------------------------------------------------
// Loaders + mappers
// ---------------------------------------------------------------------------

func loadUser(ctx context.Context, database *db.Client, id primitive.ObjectID) (*model.User, error) {
	var u db.User
	if err := database.Database.Collection(db.UsersCollection).
		FindOne(ctx, bson.M{"_id": id}).Decode(&u); err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	return userToModel(&u), nil
}

func loadUsers(ctx context.Context, database *db.Client, ids []primitive.ObjectID) (map[primitive.ObjectID]*model.User, error) {
	out := make(map[primitive.ObjectID]*model.User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	cur, err := database.Database.Collection(db.UsersCollection).
		Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, fmt.Errorf("find users: %w", err)
	}
	defer cur.Close(ctx)
	var rows []db.User
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode users: %w", err)
	}
	for i := range rows {
		out[rows[i].ID] = userToModel(&rows[i])
	}
	return out, nil
}

// loadReviewsForSnippet loads all reviews for a snippet (oldest-first, matching
// the snippetId+createdAt index) and resolves each review's author.
func loadReviewsForSnippet(ctx context.Context, database *db.Client, snippetID primitive.ObjectID) ([]*model.Review, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}})
	cur, err := database.Database.Collection(db.ReviewsCollection).
		Find(ctx, bson.M{"snippetId": snippetID}, opts)
	if err != nil {
		return nil, fmt.Errorf("find reviews: %w", err)
	}
	defer cur.Close(ctx)
	var rows []db.Review
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode reviews: %w", err)
	}
	if len(rows) == 0 {
		return []*model.Review{}, nil
	}

	authorIDs := make([]primitive.ObjectID, 0, len(rows))
	for i := range rows {
		authorIDs = append(authorIDs, rows[i].AuthorID)
	}
	authors, err := loadUsers(ctx, database, authorIDs)
	if err != nil {
		return nil, err
	}

	out := make([]*model.Review, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		author := authors[r.AuthorID]
		if author == nil {
			author = &model.User{ID: r.AuthorID.Hex(), GithubUsername: "unknown"}
		}
		out = append(out, reviewToModel(r, author))
	}
	return out, nil
}

// loadReviewIDsBySnippet returns minimal Review stubs (id-only) keyed by
// snippetId, used by the dashboard for counts without paying for full review
// payloads.
func loadReviewIDsBySnippet(ctx context.Context, database *db.Client, snippetIDs []primitive.ObjectID) (map[primitive.ObjectID][]*model.Review, error) {
	out := make(map[primitive.ObjectID][]*model.Review, len(snippetIDs))
	for _, id := range snippetIDs {
		out[id] = []*model.Review{}
	}
	if len(snippetIDs) == 0 {
		return out, nil
	}
	cur, err := database.Database.Collection(db.ReviewsCollection).Find(
		ctx,
		bson.M{"snippetId": bson.M{"$in": snippetIDs}},
		options.Find().SetProjection(bson.M{"_id": 1, "snippetId": 1}),
	)
	if err != nil {
		return nil, fmt.Errorf("find review ids: %w", err)
	}
	defer cur.Close(ctx)

	type stub struct {
		ID        primitive.ObjectID `bson:"_id"`
		SnippetID primitive.ObjectID `bson:"snippetId"`
	}
	var rows []stub
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode review ids: %w", err)
	}
	for _, s := range rows {
		out[s.SnippetID] = append(out[s.SnippetID], &model.Review{ID: s.ID.Hex()})
	}
	return out, nil
}

func userToModel(u *db.User) *model.User {
	return &model.User{
		ID:             u.ID.Hex(),
		GithubUsername: u.GitHubUsername,
		AvatarURL:      u.AvatarURL,
		CreatedAt:      u.CreatedAt,
	}
}

func reviewToModel(r *db.Review, author *model.User) *model.Review {
	var parent *string
	if r.ParentReviewID != nil {
		s := r.ParentReviewID.Hex()
		parent = &s
	}
	return &model.Review{
		ID:             r.ID.Hex(),
		SnippetID:      r.SnippetID.Hex(),
		Author:         author,
		Body:           r.Body,
		LineNumber:     r.LineNumber,
		ParentReviewID: parent,
		CreatedAt:      r.CreatedAt,
	}
}

func snippetToModel(s *db.Snippet, author *model.User, reviews []*model.Review) *model.Snippet {
	var prev *string
	if s.PreviousVersion != "" {
		p := s.PreviousVersion
		prev = &p
	}
	return &model.Snippet{
		ID:              s.ID.Hex(),
		Title:           s.Title,
		Language:        s.Language,
		Code:            s.Code,
		PreviousVersion: prev,
		Author:          author,
		Reviews:         reviews,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

func aiReviewToModel(r *ai.AIReview) *model.AIReview {
	return &model.AIReview{
		ID:            r.ID.Hex(),
		SnippetID:     r.SnippetID,
		Suggestions:   r.Suggestions,
		Complexity:    r.Complexity,
		RefactorHints: r.RefactorHints,
		SecurityFlags: r.SecurityFlags,
		QualityScore:  r.QualityScore,
		Language:      r.Language,
		GeneratedAt:   r.GeneratedAt,
	}
}

func clamp64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
