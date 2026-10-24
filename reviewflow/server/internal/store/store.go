// Package store is the only place that talks to MongoDB. It exposes domain
// types to the rest of the app and keeps BSON documents private.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"reviewflow/internal/domain"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrInvalidID = errors.New("invalid id")
)

const (
	colUsers         = "users"
	colSnippets      = "snippets"
	colReviews       = "reviews"
	colNotifications = "notifications"
	colAIReviews     = "ai_reviews"
)

type Store struct {
	client        *mongo.Client
	db            *mongo.Database
	users         *mongo.Collection
	snippets      *mongo.Collection
	reviews       *mongo.Collection
	notifications *mongo.Collection
	aiReviews     *mongo.Collection
}

// Connect dials MongoDB and verifies connectivity. It fails fast so the server
// never starts accepting traffic it cannot serve.
func Connect(ctx context.Context, uri, dbName string) (*Store, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client, err := mongo.Connect(cctx, options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(10*time.Second).
		SetConnectTimeout(10*time.Second))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(cctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo ping (is MongoDB running at the MONGO_URI?): %w", err)
	}
	db := client.Database(dbName)
	return &Store{
		client:        client,
		db:            db,
		users:         db.Collection(colUsers),
		snippets:      db.Collection(colSnippets),
		reviews:       db.Collection(colReviews),
		notifications: db.Collection(colNotifications),
		aiReviews:     db.Collection(colAIReviews),
	}, nil
}

func (s *Store) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }

func (s *Store) Ping(ctx context.Context) error { return s.client.Ping(ctx, readpref.Primary()) }

// Database exposes the handle for tests that need to clean up.
func (s *Store) Database() *mongo.Database { return s.db }

// EnsureIndexes is idempotent and safe to run on every boot.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	// Earlier versions keyed users on a unique githubId index; guests have no
	// githubId, so that index would collide on null. Drop it if it exists.
	_, _ = s.users.Indexes().DropOne(ctx, "uniq_githubId")

	specs := []struct {
		col   *mongo.Collection
		model mongo.IndexModel
	}{
		{s.users, mongo.IndexModel{
			Keys:    bson.D{{Key: "provider", Value: 1}, {Key: "providerId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_provider_providerId"),
		}},
		{s.snippets, mongo.IndexModel{
			Keys:    bson.D{{Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("createdAt_desc"),
		}},
		{s.snippets, mongo.IndexModel{
			Keys:    bson.D{{Key: "authorId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("authorId_createdAt"),
		}},
		{s.snippets, mongo.IndexModel{
			Keys:    bson.D{{Key: "language", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("language_createdAt"),
		}},
		{s.reviews, mongo.IndexModel{
			Keys:    bson.D{{Key: "snippetId", Value: 1}, {Key: "createdAt", Value: 1}},
			Options: options.Index().SetName("snippetId_createdAt"),
		}},
		{s.aiReviews, mongo.IndexModel{
			Keys:    bson.D{{Key: "snippetId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_ai_snippetId"),
		}},
		{s.notifications, mongo.IndexModel{
			Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("userId_createdAt"),
		}},
		{s.notifications, mongo.IndexModel{
			Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "read", Value: 1}},
			Options: options.Index().SetName("userId_read"),
		}},
	}
	for _, sp := range specs {
		if _, err := sp.col.Indexes().CreateOne(ctx, sp.model); err != nil {
			return fmt.Errorf("create index on %s: %w", sp.col.Name(), err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// BSON documents (private)
// ---------------------------------------------------------------------------

type userDoc struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	Provider   string             `bson:"provider"`
	ProviderID string             `bson:"providerId"`
	Username   string             `bson:"githubUsername"`
	AvatarURL  string             `bson:"avatarUrl"`
	CreatedAt  time.Time          `bson:"createdAt"`
	UpdatedAt  time.Time          `bson:"updatedAt"`
}

type snippetDoc struct {
	ID              primitive.ObjectID `bson:"_id,omitempty"`
	AuthorID        primitive.ObjectID `bson:"authorId"`
	Title           string             `bson:"title"`
	Language        string             `bson:"language"`
	Code            string             `bson:"code"`
	PreviousVersion string             `bson:"previousVersion,omitempty"`
	CreatedAt       time.Time          `bson:"createdAt"`
	UpdatedAt       time.Time          `bson:"updatedAt"`
}

type reviewDoc struct {
	ID             primitive.ObjectID  `bson:"_id,omitempty"`
	SnippetID      primitive.ObjectID  `bson:"snippetId"`
	AuthorID       primitive.ObjectID  `bson:"authorId"`
	Body           string              `bson:"body"`
	LineNumber     *int                `bson:"lineNumber,omitempty"`
	ParentReviewID *primitive.ObjectID `bson:"parentReviewId,omitempty"`
	CreatedAt      time.Time           `bson:"createdAt"`
}

type notificationDoc struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	UserID    primitive.ObjectID `bson:"userId"`
	SnippetID primitive.ObjectID `bson:"snippetId"`
	ReviewID  primitive.ObjectID `bson:"reviewId"`
	Read      bool               `bson:"read"`
	CreatedAt time.Time          `bson:"createdAt"`
}

// aiReviewDoc is keyed by snippetId (a hex *string*, as in the original schema).
// It doubles as the job-state record: pending -> ready | failed.
type aiReviewDoc struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	SnippetID     string             `bson:"snippetId"`
	Status        string             `bson:"status"`
	Error         string             `bson:"error,omitempty"`
	Suggestions   []string           `bson:"suggestions"`
	Complexity    string             `bson:"complexity"`
	RefactorHints []string           `bson:"refactorHints"`
	SecurityFlags []string           `bson:"securityFlags"`
	QualityScore  int                `bson:"qualityScore"`
	Language      string             `bson:"language"`
	GeneratedAt   time.Time          `bson:"generatedAt"`
	CreatedAt     time.Time          `bson:"createdAt"`
	UpdatedAt     time.Time          `bson:"updatedAt"`
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// now is UTC truncated to milliseconds, which is BSON's precision, so values we
// return to callers are identical to what a later read produces.
func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func oid(hex string) (primitive.ObjectID, error) {
	id, err := primitive.ObjectIDFromHex(hex)
	if err != nil {
		return primitive.NilObjectID, ErrInvalidID
	}
	return id, nil
}

func (d *userDoc) toDomain() *domain.User {
	return &domain.User{ID: d.ID.Hex(), GithubUsername: d.Username, AvatarURL: d.AvatarURL, CreatedAt: d.CreatedAt}
}

func unknownUser(id primitive.ObjectID) *domain.User {
	return &domain.User{ID: id.Hex(), GithubUsername: "unknown"}
}

// loadUsers batch-fetches users into out (keyed by hex id), skipping ones
// already present.
func (s *Store) loadUsers(ctx context.Context, ids []primitive.ObjectID, out map[string]*domain.User) error {
	var missing []primitive.ObjectID
	seen := map[primitive.ObjectID]bool{}
	for _, id := range ids {
		if _, ok := out[id.Hex()]; !ok && !seen[id] {
			seen[id] = true
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	cur, err := s.users.Find(ctx, bson.M{"_id": bson.M{"$in": missing}})
	if err != nil {
		return err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var d userDoc
		if err := cur.Decode(&d); err != nil {
			return err
		}
		out[d.ID.Hex()] = d.toDomain()
	}
	return cur.Err()
}

func userOr(users map[string]*domain.User, id primitive.ObjectID) *domain.User {
	if u, ok := users[id.Hex()]; ok {
		return u
	}
	return unknownUser(id)
}

func (d *reviewDoc) toDomain(author *domain.User) *domain.Review {
	r := &domain.Review{
		ID:         d.ID.Hex(),
		SnippetID:  d.SnippetID.Hex(),
		Author:     author,
		Body:       d.Body,
		LineNumber: d.LineNumber,
		CreatedAt:  d.CreatedAt,
	}
	if d.ParentReviewID != nil {
		p := d.ParentReviewID.Hex()
		r.ParentReviewID = &p
	}
	return r
}

func (d *aiReviewDoc) effectiveStatus() string {
	if d.Status != "" {
		return d.Status
	}
	// Rows written by earlier versions have no status but a full result.
	if d.Complexity != "" {
		return domain.AIStatusReady
	}
	return domain.AIStatusNone
}

func (d *aiReviewDoc) toDomain() *domain.AIReview {
	return &domain.AIReview{
		ID:            d.ID.Hex(),
		SnippetID:     d.SnippetID,
		Suggestions:   nonNil(d.Suggestions),
		Complexity:    d.Complexity,
		RefactorHints: nonNil(d.RefactorHints),
		SecurityFlags: nonNil(d.SecurityFlags),
		QualityScore:  d.QualityScore,
		Language:      d.Language,
		GeneratedAt:   d.GeneratedAt,
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// applyAI copies AI job state onto a snippet.
func applyAI(sn *domain.Snippet, doc *aiReviewDoc) {
	if doc == nil {
		sn.AIStatus = domain.AIStatusNone
		return
	}
	sn.AIStatus = doc.effectiveStatus()
	switch sn.AIStatus {
	case domain.AIStatusReady:
		sn.AIReview = doc.toDomain()
	case domain.AIStatusFailed:
		msg := doc.Error
		if msg == "" {
			msg = "AI review failed"
		}
		sn.AIError = &msg
	}
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

// UpsertUser inserts on first login and refreshes username/avatar afterwards.
// (provider, providerId) is the stable identity: GitHub's numeric id, or the
// lower-cased name for guest accounts.
func (s *Store) UpsertUser(ctx context.Context, provider, providerID, username, avatarURL string) (*domain.User, error) {
	t := now()
	filter := bson.M{"provider": provider, "providerId": providerID}
	update := bson.M{
		"$set":         bson.M{"githubUsername": username, "avatarUrl": avatarURL, "updatedAt": t},
		"$setOnInsert": bson.M{"createdAt": t},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var doc userDoc
	err := s.users.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if mongo.IsDuplicateKeyError(err) {
		// Two first-time logins raced; the loser's upsert collides. Retry once.
		err = s.users.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	}
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return doc.toDomain(), nil
}

func (s *Store) GetUser(ctx context.Context, id string) (*domain.User, error) {
	o, err := oid(id)
	if err != nil {
		return nil, err
	}
	var d userDoc
	if err := s.users.FindOne(ctx, bson.M{"_id": o}).Decode(&d); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return d.toDomain(), nil
}

// ---------------------------------------------------------------------------
// Snippets
// ---------------------------------------------------------------------------

type NewSnippet struct {
	AuthorID        string
	Title           string
	Language        string
	Code            string
	PreviousVersion *string
}

// SnippetSource is the lightweight view the AI pipeline and review flow need.
type SnippetSource struct {
	ID       string
	AuthorID string
	Title    string
	Language string
	Code     string
}

func (s *Store) CreateSnippet(ctx context.Context, in NewSnippet) (*domain.Snippet, error) {
	authorOID, err := oid(in.AuthorID)
	if err != nil {
		return nil, err
	}
	author, err := s.GetUser(ctx, in.AuthorID)
	if err != nil {
		return nil, fmt.Errorf("load author: %w", err)
	}
	t := now()
	doc := snippetDoc{
		AuthorID: authorOID, Title: in.Title, Language: in.Language, Code: in.Code,
		CreatedAt: t, UpdatedAt: t,
	}
	if in.PreviousVersion != nil {
		doc.PreviousVersion = *in.PreviousVersion
	}
	res, err := s.snippets.InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("insert snippet: %w", err)
	}
	doc.ID = res.InsertedID.(primitive.ObjectID)
	return snippetToDomain(&doc, author), nil
}

func snippetToDomain(d *snippetDoc, author *domain.User) *domain.Snippet {
	sn := &domain.Snippet{
		ID: d.ID.Hex(), Title: d.Title, Language: d.Language, Code: d.Code,
		Author: author, Reviews: []*domain.Review{}, AIStatus: domain.AIStatusNone,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
	if d.PreviousVersion != "" {
		p := d.PreviousVersion
		sn.PreviousVersion = &p
	}
	return sn
}

// GetSnippet loads one snippet with its author, all reviews (with authors) and
// AI state.
func (s *Store) GetSnippet(ctx context.Context, id string) (*domain.Snippet, error) {
	o, err := oid(id)
	if err != nil {
		return nil, err
	}
	var d snippetDoc
	if err := s.snippets.FindOne(ctx, bson.M{"_id": o}).Decode(&d); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	reviews, err := s.findReviews(ctx, []primitive.ObjectID{o})
	if err != nil {
		return nil, err
	}
	users := map[string]*domain.User{}
	ids := []primitive.ObjectID{d.AuthorID}
	for _, r := range reviews {
		ids = append(ids, r.AuthorID)
	}
	if err := s.loadUsers(ctx, ids, users); err != nil {
		return nil, err
	}

	sn := snippetToDomain(&d, userOr(users, d.AuthorID))
	for _, r := range reviews {
		sn.Reviews = append(sn.Reviews, r.toDomain(userOr(users, r.AuthorID)))
	}
	aiDocs, err := s.findAI(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	applyAI(sn, aiDocs[id])
	return sn, nil
}

type ListFilter struct {
	AuthorID string
	Language string
	Limit    int
}

// ListSnippets returns newest-first snippets. Authors, reviews and AI rows are
// batch-loaded, so the query count is constant (5) regardless of result size.
func (s *Store) ListSnippets(ctx context.Context, f ListFilter) ([]*domain.Snippet, error) {
	filter := bson.M{}
	if f.AuthorID != "" {
		a, err := oid(f.AuthorID)
		if err != nil {
			return nil, err
		}
		filter["authorId"] = a
	}
	if f.Language != "" {
		filter["language"] = f.Language
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	cur, err := s.snippets.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	var docs []snippetDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return []*domain.Snippet{}, nil
	}

	oids := make([]primitive.ObjectID, len(docs))
	hexes := make([]string, len(docs))
	for i, d := range docs {
		oids[i], hexes[i] = d.ID, d.ID.Hex()
	}
	reviews, err := s.findReviews(ctx, oids)
	if err != nil {
		return nil, err
	}
	aiDocs, err := s.findAI(ctx, hexes)
	if err != nil {
		return nil, err
	}

	userIDs := make([]primitive.ObjectID, 0, len(docs)+len(reviews))
	for _, d := range docs {
		userIDs = append(userIDs, d.AuthorID)
	}
	for _, r := range reviews {
		userIDs = append(userIDs, r.AuthorID)
	}
	users := map[string]*domain.User{}
	if err := s.loadUsers(ctx, userIDs, users); err != nil {
		return nil, err
	}

	bySnippet := map[primitive.ObjectID][]*domain.Review{}
	for i := range reviews {
		r := &reviews[i]
		bySnippet[r.SnippetID] = append(bySnippet[r.SnippetID], r.toDomain(userOr(users, r.AuthorID)))
	}

	out := make([]*domain.Snippet, 0, len(docs))
	for i := range docs {
		d := &docs[i]
		sn := snippetToDomain(d, userOr(users, d.AuthorID))
		if rs := bySnippet[d.ID]; rs != nil {
			sn.Reviews = rs
		}
		applyAI(sn, aiDocs[d.ID.Hex()])
		out = append(out, sn)
	}
	return out, nil
}

func (s *Store) SnippetSource(ctx context.Context, id string) (*SnippetSource, error) {
	o, err := oid(id)
	if err != nil {
		return nil, err
	}
	var d snippetDoc
	err = s.snippets.FindOne(ctx, bson.M{"_id": o}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &SnippetSource{ID: d.ID.Hex(), AuthorID: d.AuthorID.Hex(), Title: d.Title, Language: d.Language, Code: d.Code}, nil
}

// SnippetExists is used by the WebSocket endpoint so arbitrary ids cannot
// create empty rooms.
func (s *Store) SnippetExists(ctx context.Context, id string) bool {
	o, err := oid(id)
	if err != nil {
		return false
	}
	n, err := s.snippets.CountDocuments(ctx, bson.M{"_id": o}, options.Count().SetLimit(1))
	return err == nil && n > 0
}

// ---------------------------------------------------------------------------
// Reviews
// ---------------------------------------------------------------------------

func (s *Store) findReviews(ctx context.Context, snippetIDs []primitive.ObjectID) ([]reviewDoc, error) {
	cur, err := s.reviews.Find(ctx, bson.M{"snippetId": bson.M{"$in": snippetIDs}},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []reviewDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

type NewReview struct {
	SnippetID      string
	AuthorID       string
	Body           string
	LineNumber     *int
	ParentReviewID *string
}

// AddReview inserts a review. Threads are capped at depth 1: replying to a
// reply attaches to that reply's parent instead.
func (s *Store) AddReview(ctx context.Context, in NewReview) (*domain.Review, error) {
	snippetOID, err := oid(in.SnippetID)
	if err != nil {
		return nil, err
	}
	authorOID, err := oid(in.AuthorID)
	if err != nil {
		return nil, err
	}
	author, err := s.GetUser(ctx, in.AuthorID)
	if err != nil {
		return nil, fmt.Errorf("load author: %w", err)
	}

	doc := reviewDoc{
		SnippetID: snippetOID, AuthorID: authorOID, Body: in.Body,
		LineNumber: in.LineNumber, CreatedAt: now(),
	}
	if in.ParentReviewID != nil && *in.ParentReviewID != "" {
		parentOID, err := oid(*in.ParentReviewID)
		if err != nil {
			return nil, fmt.Errorf("invalid parentReviewId: %w", err)
		}
		var parent reviewDoc
		err = s.reviews.FindOne(ctx, bson.M{"_id": parentOID, "snippetId": snippetOID}).Decode(&parent)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errors.New("parent review not found for this snippet")
		}
		if err != nil {
			return nil, err
		}
		top := parentOID
		if parent.ParentReviewID != nil {
			top = *parent.ParentReviewID
		}
		doc.ParentReviewID = &top
		doc.LineNumber = nil // a reply is never anchored to a line
	}

	res, err := s.reviews.InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("insert review: %w", err)
	}
	doc.ID = res.InsertedID.(primitive.ObjectID)
	return doc.toDomain(author), nil
}

// ---------------------------------------------------------------------------
// Notifications
// ---------------------------------------------------------------------------

func (s *Store) CreateNotification(ctx context.Context, userID, snippetID, reviewID string) error {
	u, err := oid(userID)
	if err != nil {
		return err
	}
	sn, err := oid(snippetID)
	if err != nil {
		return err
	}
	rv, err := oid(reviewID)
	if err != nil {
		return err
	}
	_, err = s.notifications.InsertOne(ctx, notificationDoc{
		UserID: u, SnippetID: sn, ReviewID: rv, Read: false, CreatedAt: now(),
	})
	return err
}

func (s *Store) ListNotifications(ctx context.Context, userID string, page, pageSize int) ([]domain.Notification, error) {
	u, err := oid(userID)
	if err != nil {
		return nil, err
	}
	cur, err := s.notifications.Find(ctx, bson.M{"userId": u}, options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(int64(page*pageSize)).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, err
	}
	var docs []notificationDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]domain.Notification, 0, len(docs))
	if len(docs) == 0 {
		return out, nil
	}

	// Batch-load the snippet titles.
	idSet := map[primitive.ObjectID]bool{}
	var ids []primitive.ObjectID
	for _, d := range docs {
		if !idSet[d.SnippetID] {
			idSet[d.SnippetID] = true
			ids = append(ids, d.SnippetID)
		}
	}
	titles := map[primitive.ObjectID]string{}
	tc, err := s.snippets.Find(ctx, bson.M{"_id": bson.M{"$in": ids}},
		options.Find().SetProjection(bson.M{"title": 1}))
	if err != nil {
		return nil, err
	}
	var tdocs []snippetDoc
	if err := tc.All(ctx, &tdocs); err != nil {
		return nil, err
	}
	for _, t := range tdocs {
		titles[t.ID] = t.Title
	}

	for _, d := range docs {
		out = append(out, domain.Notification{
			ID: d.ID.Hex(), SnippetID: d.SnippetID.Hex(), SnippetTitle: titles[d.SnippetID],
			ReviewID: d.ReviewID.Hex(), Read: d.Read, CreatedAt: d.CreatedAt,
		})
	}
	return out, nil
}

func (s *Store) UnreadCount(ctx context.Context, userID string) (int64, error) {
	u, err := oid(userID)
	if err != nil {
		return 0, err
	}
	return s.notifications.CountDocuments(ctx, bson.M{"userId": u, "read": false})
}

func (s *Store) MarkAllRead(ctx context.Context, userID string) error {
	u, err := oid(userID)
	if err != nil {
		return err
	}
	_, err = s.notifications.UpdateMany(ctx, bson.M{"userId": u, "read": false}, bson.M{"$set": bson.M{"read": true}})
	return err
}

// ---------------------------------------------------------------------------
// AI review state
// ---------------------------------------------------------------------------

func (s *Store) findAI(ctx context.Context, snippetIDs []string) (map[string]*aiReviewDoc, error) {
	cur, err := s.aiReviews.Find(ctx, bson.M{"snippetId": bson.M{"$in": snippetIDs}})
	if err != nil {
		return nil, err
	}
	var docs []aiReviewDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make(map[string]*aiReviewDoc, len(docs))
	for i := range docs {
		out[docs[i].SnippetID] = &docs[i]
	}
	return out, nil
}

// MarkAIPending records that a review is queued. Upserting on snippetId makes
// it safe to call repeatedly (retries, restarts).
func (s *Store) MarkAIPending(ctx context.Context, snippetID string) error {
	t := now()
	_, err := s.aiReviews.UpdateOne(ctx, bson.M{"snippetId": snippetID},
		bson.M{
			"$set":         bson.M{"status": domain.AIStatusPending, "error": "", "updatedAt": t},
			"$setOnInsert": bson.M{"createdAt": t},
		}, options.Update().SetUpsert(true))
	return err
}

// SaveAIReady is the idempotent upsert at the end of the pipeline: one row per
// snippet, replaced wholesale, so re-running a review never duplicates it.
func (s *Store) SaveAIReady(ctx context.Context, r *domain.AIReview) (*domain.AIReview, error) {
	t := now()
	set := bson.M{
		"status":        domain.AIStatusReady,
		"error":         "",
		"suggestions":   nonNil(r.Suggestions),
		"complexity":    r.Complexity,
		"refactorHints": nonNil(r.RefactorHints),
		"securityFlags": nonNil(r.SecurityFlags),
		"qualityScore":  r.QualityScore,
		"language":      r.Language,
		"generatedAt":   t,
		"updatedAt":     t,
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var doc aiReviewDoc
	err := s.aiReviews.FindOneAndUpdate(ctx, bson.M{"snippetId": r.SnippetID},
		bson.M{"$set": set, "$setOnInsert": bson.M{"createdAt": t}}, opts).Decode(&doc)
	if mongo.IsDuplicateKeyError(err) {
		err = s.aiReviews.FindOneAndUpdate(ctx, bson.M{"snippetId": r.SnippetID},
			bson.M{"$set": set, "$setOnInsert": bson.M{"createdAt": t}}, opts).Decode(&doc)
	}
	if err != nil {
		return nil, fmt.Errorf("save ai review: %w", err)
	}
	return doc.toDomain(), nil
}

func (s *Store) MarkAIFailed(ctx context.Context, snippetID, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) > 500 {
		reason = reason[:500]
	}
	t := now()
	_, err := s.aiReviews.UpdateOne(ctx, bson.M{"snippetId": snippetID},
		bson.M{
			"$set":         bson.M{"status": domain.AIStatusFailed, "error": reason, "updatedAt": t},
			"$setOnInsert": bson.M{"createdAt": t},
		}, options.Update().SetUpsert(true))
	return err
}

// PendingAISnippets lists snippets whose review was queued but never finished
// (for example the server restarted), so the service can re-queue them.
func (s *Store) PendingAISnippets(ctx context.Context, limit int) ([]string, error) {
	cur, err := s.aiReviews.Find(ctx, bson.M{"status": domain.AIStatusPending},
		options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "updatedAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []aiReviewDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.SnippetID)
	}
	return out, nil
}
