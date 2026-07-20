import type { AIService } from '../ai/service.ts';
import type { DBClient } from '../db/client.ts';
import type { Hub } from '../ws/hub.ts';

/**
 * What every resolver receives. `userId` is populated by the soft auth
 * middleware when a valid session token is present, and is undefined for
 * anonymous callers — resolvers decide which operations require it.
 */
// Declared as a type alias rather than an interface on purpose: graphql-http's
// OperationContext is a Record<PropertyKey, unknown>, and only type aliases get
// the implicit index signature that makes them assignable to it.
export type GraphQLContext = {
  db: DBClient;
  hub: Hub | null;
  ai: AIService | null;
  userId?: string;
};

// --- Shapes returned by resolvers (the GraphQL layer's view of the domain) ---

export interface UserModel {
  id: string;
  githubUsername: string;
  avatarUrl: string;
  createdAt: Date;
}

export interface ReviewModel {
  id: string;
  snippetId: string;
  author: UserModel;
  body: string;
  lineNumber: number | null;
  parentReviewId: string | null;
  createdAt: Date;
}

/**
 * An id-only review, returned by the dashboard list query — which asks for
 * `reviews { id }` and nothing else, so paying for full review payloads (and
 * their author joins) would be wasted work.
 */
export interface ReviewStub {
  id: string;
}

export interface SnippetModel {
  id: string;
  title: string;
  language: string;
  code: string;
  previousVersion: string | null;
  author: UserModel;
  reviews: Array<ReviewModel | ReviewStub>;
  createdAt: Date;
  updatedAt: Date;
}

export interface CreateSnippetInput {
  title: string;
  language: string;
  code: string;
  previousVersion?: string | null;
}

export interface AddReviewInput {
  body: string;
  lineNumber?: number | null;
  parentReviewId?: string | null;
}
