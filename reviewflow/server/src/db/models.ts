import type { Db, ObjectId, OptionalUnlessRequiredId } from 'mongodb';

/**
 * Collection names are centralised so handlers/resolvers never hard-code
 * strings (a typo would silently target the wrong collection).
 */
export const UsersCollection = 'users';
export const SnippetsCollection = 'snippets';
export const ReviewsCollection = 'reviews';
export const NotificationsCollection = 'notifications';
export const AIReviewsCollection = 'ai_reviews';

/**
 * A GitHub-authenticated account. The Mongo _id is our internal user id
 * (surfaced to the API as a hex string); githubId is GitHub's numeric id and is
 * the stable key we upsert on (usernames can change, ids don't).
 */
export interface UserDoc {
  _id: ObjectId;
  githubId: number;
  githubUsername: string;
  avatarUrl: string;
  createdAt: Date;
  updatedAt: Date;
}

/**
 * A piece of code submitted for review. previousVersion is the prior code
 * (optional); when present, the UI offers a diff view.
 */
export interface SnippetDoc {
  _id: ObjectId;
  authorId: ObjectId;
  title: string;
  language: string;
  code: string;
  previousVersion?: string;
  createdAt: Date;
  updatedAt: Date;
}

/**
 * A human review comment on a snippet. lineNumber is optional so a general,
 * non-line-anchored comment is distinguishable from a comment on line 0.
 * parentReviewId, when set, marks this as a reply to a top-level review (the
 * resolver enforces max depth = 1).
 */
export interface ReviewDoc {
  _id: ObjectId;
  snippetId: ObjectId;
  authorId: ObjectId;
  body: string;
  lineNumber?: number | null;
  parentReviewId?: ObjectId | null;
  createdAt: Date;
}

/** Dropped into the bell whenever someone reviews YOUR snippet. */
export interface NotificationDoc {
  _id: ObjectId;
  userId: ObjectId; // recipient
  snippetId: ObjectId;
  reviewId: ObjectId;
  read: boolean;
  createdAt: Date;
}

/**
 * The AI review document. snippetId is stored as a hex *string* (not an
 * ObjectId) — matching the original Go schema, so existing rows keep working.
 */
export interface AIReviewDoc {
  _id: ObjectId;
  snippetId: string;
  suggestions: string[];
  complexity: string;
  refactorHints: string[];
  securityFlags: string[];
  qualityScore: number;
  language: string;
  generatedAt: Date;
}

/**
 * Adapts a document we're about to insert to what the driver's insertOne wants.
 *
 * Our document types declare `_id` as required, which is the truth on *read*;
 * on *write* Mongo assigns it. This is the one place that reconciles the two.
 */
export function newDoc<T extends { _id: ObjectId }>(doc: Omit<T, '_id'>): OptionalUnlessRequiredId<T> {
  return doc as OptionalUnlessRequiredId<T>;
}

/**
 * Creates every index the app relies on. Safe to call on each startup:
 * createIndex is idempotent when the spec (keys + options) matches an existing
 * index. Call once after connect, before serving traffic.
 */
export async function ensureIndexes(database: Db): Promise<void> {
  // users.githubId UNIQUE: one account per GitHub identity, and a fast lookup
  // for the OAuth upsert.
  await database
    .collection(UsersCollection)
    .createIndex({ githubId: 1 }, { unique: true, name: 'uniq_githubId' });

  // snippets {authorId:1, createdAt:-1}: list one author's snippets newest-first.
  await database
    .collection(SnippetsCollection)
    .createIndex({ authorId: 1, createdAt: -1 }, { name: 'authorId_createdAt' });

  // reviews {snippetId:1, createdAt:1}: fetch a snippet's reviews in
  // conversation (oldest-first) order.
  await database
    .collection(ReviewsCollection)
    .createIndex({ snippetId: 1, createdAt: 1 }, { name: 'snippetId_createdAt' });

  // ai_reviews.snippetId UNIQUE: one AI review per snippet, and the key the AI
  // service upserts on (replacing a prior review on re-run).
  await database
    .collection(AIReviewsCollection)
    .createIndex({ snippetId: 1 }, { unique: true, name: 'uniq_ai_snippetId' });

  // notifications: each user's bell, newest-first, plus an unread-count index.
  await database.collection(NotificationsCollection).createIndexes([
    { key: { userId: 1, createdAt: -1 }, name: 'userId_createdAt' },
    { key: { userId: 1, read: 1 }, name: 'userId_read' },
  ]);
}
