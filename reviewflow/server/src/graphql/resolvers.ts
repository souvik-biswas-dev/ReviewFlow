/**
 * The resolver map. Every entry delegates straight to snippet.ts, mirroring the
 * split the Go backend had between generated resolver stubs and hand-written
 * logic.
 */
import type { AIReview } from '../ai/types.ts';
import {
  addReview,
  aiReviewForSnippet,
  createSnippet,
  getSnippet,
  listSnippets,
  me,
} from './snippet.ts';
import type {
  AddReviewInput,
  CreateSnippetInput,
  GraphQLContext,
  ReviewModel,
  SnippetModel,
  UserModel,
} from './types.ts';

export const resolvers = {
  Query: {
    snippet: (_p: unknown, args: { id: string }, ctx: GraphQLContext): Promise<SnippetModel | null> =>
      getSnippet(ctx, args.id),

    snippets: (
      _p: unknown,
      args: { authorId?: string | null; language?: string | null; limit?: number | null },
      ctx: GraphQLContext,
    ): Promise<SnippetModel[]> => listSnippets(ctx, args.authorId, args.language, args.limit),

    me: (_p: unknown, _args: unknown, ctx: GraphQLContext): Promise<UserModel | null> => me(ctx),
  },

  Mutation: {
    /** Saves the snippet synchronously, fires an async AI review, returns immediately. */
    createSnippet: (
      _p: unknown,
      args: { input: CreateSnippetInput },
      ctx: GraphQLContext,
    ): Promise<SnippetModel> => createSnippet(ctx, args.input),

    /**
     * Inserts a review, broadcasts review_added to the snippet's room, and
     * drops a notification in the snippet author's bell.
     */
    addReview: (
      _p: unknown,
      args: { snippetId: string; input: AddReviewInput },
      ctx: GraphQLContext,
    ): Promise<ReviewModel> => addReview(ctx, args.snippetId, args.input),
  },

  Snippet: {
    /** Lazily loads the stored AI review for this snippet (null until generated). */
    aiReview: (
      parent: SnippetModel,
      _args: unknown,
      ctx: GraphQLContext,
    ): Promise<AIReview | null> => aiReviewForSnippet(ctx, parent.id),
  },

  Subscription: {
    /**
     * Browser clients receive review_added and presence over the WebSocket hub
     * (src/ws), which the mutations publish to. These GraphQL subscriptions
     * would be a typed alternative for GraphQL-over-WS clients; they remain
     * stubs — an immediately-completed stream — because the WS hub already
     * covers the realtime path the frontend uses.
     */
    reviewAdded: { subscribe: emptyStream },
    presenceUpdated: { subscribe: emptyStream },
  },
};

async function* emptyStream(): AsyncGenerator<never, void, unknown> {
  // Yields nothing and completes immediately.
}
