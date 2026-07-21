/**
 * The hand-written business logic behind the GraphQL resolvers. The resolver
 * map (resolvers.ts) stays thin and delegates here, which keeps the schema
 * wiring and the domain logic separate.
 */
import { ObjectId, type Filter } from 'mongodb';
import { docToAIReview, type AIReview } from '../ai/types.ts';
import type { DBClient } from '../db/client.ts';
import { newDoc, type NotificationDoc, type ReviewDoc, type SnippetDoc, type UserDoc } from '../db/models.ts';
import type { Hub } from '../ws/hub.ts';
import { MessageType, newMessage } from '../ws/message.ts';
import type { AIService } from '../ai/service.ts';
import type {
  AddReviewInput,
  CreateSnippetInput,
  GraphQLContext,
  ReviewModel,
  ReviewStub,
  SnippetModel,
  UserModel,
} from './types.ts';

/** Thrown when a mutation requires a logged-in user. */
const ERR_UNAUTHENTICATED = 'authentication required';

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

/**
 * Persists a new snippet authored by the current user, kicks off an
 * asynchronous AI review, and returns the snippet immediately — the frontend
 * learns the AI review is ready later via the WebSocket "ai_review_ready" event.
 */
export async function createSnippet(
  ctx: GraphQLContext,
  input: CreateSnippetInput,
): Promise<SnippetModel> {
  const authorId = callerObjectId(ctx);

  if (!input.title?.trim() || !input.language?.trim() || !input.code?.trim()) {
    throw new Error('title, language and code are required');
  }

  const now = new Date();
  const doc: Omit<SnippetDoc, '_id'> = {
    authorId,
    title: input.title,
    language: input.language,
    code: input.code,
    createdAt: now,
    updatedAt: now,
  };
  if (input.previousVersion) doc.previousVersion = input.previousVersion;

  // 1. Save synchronously.
  const res = await ctx.db.snippets.insertOne(newDoc<SnippetDoc>(doc));
  const snippet: SnippetDoc = { ...doc, _id: res.insertedId };

  // 2. Load the author (the schema's `author` field is non-null).
  const author = await loadUser(ctx.db, authorId);

  // 3. Fire-and-forget the AI review: it must not hold up the mutation, and it
  //    applies its own 30s timeout internally.
  scheduleAIReview(ctx.ai, snippet);

  return snippetToModel(snippet, author, []);
}

/**
 * Inserts a human review, broadcasts it to the snippet's room over the WS hub,
 * and drops a notification in the snippet author's bell.
 *
 * Threading: if input.parentReviewId names a reply, we flatten to 1 level —
 * replies always attach to a top-level review, never to another reply.
 */
export async function addReview(
  ctx: GraphQLContext,
  snippetId: string,
  input: AddReviewInput,
): Promise<ReviewModel> {
  const authorId = callerObjectId(ctx);

  if (!input.body?.trim()) throw new Error('review body is required');

  const snippetOID = toObjectId(snippetId, 'invalid snippet id');

  // Load the snippet — needed for the notification (snippet author).
  const snippet = await ctx.db.snippets.findOne({ _id: snippetOID });
  if (!snippet) throw new Error('snippet not found');

  // Resolve the parent (enforces max thread depth of 1).
  const parentReviewId = await resolveParent(ctx.db, snippetOID, input.parentReviewId);

  const now = new Date();
  const doc: Omit<ReviewDoc, '_id'> = {
    snippetId: snippetOID,
    authorId,
    body: input.body.trim(),
    createdAt: now,
  };
  if (input.lineNumber !== undefined && input.lineNumber !== null) doc.lineNumber = input.lineNumber;
  if (parentReviewId) doc.parentReviewId = parentReviewId;

  const res = await ctx.db.reviews.insertOne(newDoc<ReviewDoc>(doc));
  const review: ReviewDoc = { ...doc, _id: res.insertedId };

  const author = await loadUser(ctx.db, authorId);
  const model = reviewToModel(review, author);

  // Broadcast to everyone in the snippet's WS room. The reviewer's own client
  // also receives this and de-duplicates by id against the optimistic add.
  broadcastReview(ctx.hub, snippetId, model);

  // Notify the snippet's author — but only when someone else is reviewing.
  if (!snippet.authorId.equals(authorId)) {
    try {
      await ctx.db.notifications.insertOne(
        newDoc<NotificationDoc>({
          userId: snippet.authorId,
          snippetId: snippetOID,
          reviewId: review._id,
          read: false,
          createdAt: now,
        }),
      );
    } catch (err) {
      // Best-effort: don't fail the mutation if the bell fails.
      console.error('notifications: insert failed:', err);
    }
  }

  return model;
}

/**
 * Validates a parent review and flattens nested replies up one level so the
 * thread never exceeds depth 1.
 */
async function resolveParent(
  db: DBClient,
  snippetOID: ObjectId,
  parentId: string | null | undefined,
): Promise<ObjectId | null> {
  if (!parentId) return null;

  const parentOID = toObjectId(parentId, 'invalid parentReviewId');
  const parent = await db.reviews.findOne({ _id: parentOID, snippetId: snippetOID });
  if (!parent) throw new Error('parent review not found for this snippet');

  // If the "parent" is itself a reply, attach to its parent instead.
  return parent.parentReviewId ?? parentOID;
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

/** The currently authenticated user, or null when anonymous. */
export async function me(ctx: GraphQLContext): Promise<UserModel | null> {
  if (!ctx.userId) return null;
  let oid: ObjectId;
  try {
    oid = new ObjectId(ctx.userId);
  } catch {
    return null;
  }
  return loadUser(ctx.db, oid);
}

/**
 * Loads one snippet with its author + the full flat list of reviews (each with
 * its author). Returns null on not-found → GraphQL null.
 */
export async function getSnippet(ctx: GraphQLContext, id: string): Promise<SnippetModel | null> {
  let oid: ObjectId;
  try {
    oid = new ObjectId(id);
  } catch {
    return null;
  }

  const snippet = await ctx.db.snippets.findOne({ _id: oid });
  if (!snippet) return null;

  const [author, reviews] = await Promise.all([
    loadUser(ctx.db, snippet.authorId),
    loadReviewsForSnippet(ctx.db, snippet._id),
  ]);

  return snippetToModel(snippet, author, reviews);
}

/**
 * Backs the dashboard. It batch-loads authors and review-id lists in two
 * queries rather than per-snippet (no N+1).
 */
export async function listSnippets(
  ctx: GraphQLContext,
  authorId?: string | null,
  language?: string | null,
  limit?: number | null,
): Promise<SnippetModel[]> {
  const filter: Filter<SnippetDoc> = {};
  if (authorId) {
    try {
      filter.authorId = new ObjectId(authorId);
    } catch {
      // An unparseable author id simply doesn't narrow the query, matching the
      // original behavior.
    }
  }
  if (language) filter.language = language;

  const lim = limit == null ? 20 : clamp(Math.trunc(limit), 1, 100);

  const rows = await ctx.db.snippets.find(filter).sort({ createdAt: -1 }).limit(lim).toArray();
  if (rows.length === 0) return [];

  const authorIds = rows.map((r) => r.authorId);
  const snippetIds = rows.map((r) => r._id);

  // Batch-load all authors, and the review ids per snippet (the dashboard only
  // needs counts), in one round-trip each.
  const [authors, reviewIds] = await Promise.all([
    loadUsers(ctx.db, authorIds),
    loadReviewIdsBySnippet(ctx.db, snippetIds),
  ]);

  return rows.map((row) => {
    const author = authors.get(row.authorId.toHexString()) ?? unknownUser(row.authorId);
    return snippetToModel(row, author, reviewIds.get(row._id.toHexString()) ?? []);
  });
}

/**
 * Backs the Snippet.aiReview field resolver. Returns null when the AI pass
 * hasn't finished yet, which the frontend renders as "AI analyzing...".
 */
export async function aiReviewForSnippet(
  ctx: GraphQLContext,
  snippetId: string,
): Promise<AIReview | null> {
  const doc = await ctx.db.aiReviews.findOne({ snippetId });
  return doc ? docToAIReview(doc) : null;
}

// ---------------------------------------------------------------------------
// Side effects
// ---------------------------------------------------------------------------

/**
 * Runs the AI review detached from the request. Errors are logged, never
 * propagated: a failed analysis leaves `aiReview` null, which the UI handles.
 */
function scheduleAIReview(ai: AIService | null, snippet: SnippetDoc): void {
  if (!ai) return;
  const snippetId = snippet._id.toHexString();
  void ai.reviewCode(snippetId, snippet.language, snippet.code).catch((err: unknown) => {
    console.error(`ai: review for snippet ${snippetId} failed:`, err);
  });
}

function broadcastReview(hub: Hub | null, snippetId: string, review: ReviewModel): void {
  if (!hub) return;
  hub.broadcastToRoom(snippetId, newMessage(MessageType.ReviewAdded, snippetId, review));
}

// ---------------------------------------------------------------------------
// Loaders + mappers
// ---------------------------------------------------------------------------

async function loadUser(db: DBClient, id: ObjectId): Promise<UserModel> {
  const user = await db.users.findOne({ _id: id });
  if (!user) throw new Error(`load user: ${id.toHexString()} not found`);
  return userToModel(user);
}

/** Batch-loads users, keyed by hex id (ObjectId isn't usable as a Map key). */
async function loadUsers(db: DBClient, ids: ObjectId[]): Promise<Map<string, UserModel>> {
  const out = new Map<string, UserModel>();
  if (ids.length === 0) return out;

  const rows = await db.users.find({ _id: { $in: ids } }).toArray();
  for (const row of rows) out.set(row._id.toHexString(), userToModel(row));
  return out;
}

/**
 * Loads all reviews for a snippet (oldest-first, matching the
 * snippetId+createdAt index) and resolves each review's author.
 */
async function loadReviewsForSnippet(db: DBClient, snippetId: ObjectId): Promise<ReviewModel[]> {
  const rows = await db.reviews.find({ snippetId }).sort({ createdAt: 1 }).toArray();
  if (rows.length === 0) return [];

  const authors = await loadUsers(
    db,
    rows.map((r) => r.authorId),
  );

  return rows.map((row) =>
    reviewToModel(row, authors.get(row.authorId.toHexString()) ?? unknownUser(row.authorId)),
  );
}

/**
 * Returns id-only review stubs keyed by snippet hex id, used by the dashboard
 * for counts without paying for full review payloads.
 */
async function loadReviewIdsBySnippet(
  db: DBClient,
  snippetIds: ObjectId[],
): Promise<Map<string, ReviewStub[]>> {
  const out = new Map<string, ReviewStub[]>();
  for (const id of snippetIds) out.set(id.toHexString(), []);
  if (snippetIds.length === 0) return out;

  const rows = await db.reviews
    .find({ snippetId: { $in: snippetIds } })
    .project<{ _id: ObjectId; snippetId: ObjectId }>({ _id: 1, snippetId: 1 })
    .toArray();

  for (const row of rows) {
    out.get(row.snippetId.toHexString())?.push({ id: row._id.toHexString() });
  }
  return out;
}

function userToModel(u: UserDoc): UserModel {
  return {
    id: u._id.toHexString(),
    githubUsername: u.githubUsername,
    avatarUrl: u.avatarUrl,
    createdAt: u.createdAt,
  };
}

function reviewToModel(r: ReviewDoc, author: UserModel): ReviewModel {
  return {
    id: r._id.toHexString(),
    snippetId: r.snippetId.toHexString(),
    author,
    body: r.body,
    lineNumber: r.lineNumber ?? null,
    parentReviewId: r.parentReviewId ? r.parentReviewId.toHexString() : null,
    createdAt: r.createdAt,
  };
}

function snippetToModel(
  s: SnippetDoc,
  author: UserModel,
  reviews: Array<ReviewModel | ReviewStub>,
): SnippetModel {
  return {
    id: s._id.toHexString(),
    title: s.title,
    language: s.language,
    code: s.code,
    previousVersion: s.previousVersion ? s.previousVersion : null,
    author,
    reviews,
    createdAt: s.createdAt,
    updatedAt: s.updatedAt,
  };
}

/** Placeholder for a review/snippet whose author row has gone missing. */
function unknownUser(id: ObjectId): UserModel {
  return {
    id: id.toHexString(),
    githubUsername: 'unknown',
    avatarUrl: '',
    createdAt: new Date(0),
  };
}

// --- small helpers -----------------------------------------------------------

/** The caller's ObjectId, or an authentication error when anonymous. */
function callerObjectId(ctx: GraphQLContext): ObjectId {
  if (!ctx.userId) throw new Error(ERR_UNAUTHENTICATED);
  try {
    return new ObjectId(ctx.userId);
  } catch {
    throw new Error(ERR_UNAUTHENTICATED);
  }
}

function toObjectId(hex: string, errMessage: string): ObjectId {
  try {
    return new ObjectId(hex);
  } catch {
    throw new Error(errMessage);
  }
}

function clamp(v: number, lo: number, hi: number): number {
  if (Number.isNaN(v)) return lo;
  return Math.min(hi, Math.max(lo, v));
}
