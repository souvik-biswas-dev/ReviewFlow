import type { AIReviewDoc } from '../db/models.ts';

/**
 * Caps enforced on the model's output (also requested in the prompt, but we
 * clamp defensively in case the model overshoots).
 */
export const MAX_SUGGESTIONS = 5;
export const MAX_REFACTOR_HINTS = 3;

/**
 * A generated review as the rest of the system sees it: the JSON payload sent
 * over the WebSocket "ai_review_ready" event and the source for the GraphQL
 * mapping. `id` is empty until the document has been persisted.
 */
export interface AIReview {
  id: string;
  snippetId: string;
  suggestions: string[];
  complexity: string;
  refactorHints: string[];
  securityFlags: string[];
  qualityScore: number;
  language: string;
  generatedAt: Date;
}

/** The stored document minus its Mongo-assigned _id. */
export type AIReviewFields = Omit<AIReviewDoc, '_id'>;

/**
 * The exact JSON shape we instruct Gemini to return. It carries only the
 * model-authored fields; we add snippetId/language/generatedAt ourselves so the
 * model can't get them wrong.
 */
export interface GeminiReview {
  suggestions?: unknown;
  complexity?: unknown;
  refactorHints?: unknown;
  securityFlags?: unknown;
  qualityScore?: unknown;
}

/**
 * Builds the stored review from raw model output, normalizing it: non-array or
 * missing lists become empty arrays (so GraphQL's non-null lists are satisfied)
 * and the length caps are enforced.
 */
export function toAIReviewFields(g: GeminiReview, snippetId: string, language: string): AIReviewFields {
  return {
    snippetId,
    suggestions: clampStrings(g.suggestions, MAX_SUGGESTIONS),
    complexity: typeof g.complexity === 'string' ? g.complexity : '',
    refactorHints: clampStrings(g.refactorHints, MAX_REFACTOR_HINTS),
    securityFlags: clampStrings(g.securityFlags, Number.MAX_SAFE_INTEGER),
    qualityScore: typeof g.qualityScore === 'number' ? g.qualityScore : NaN,
    language,
    generatedAt: new Date(),
  };
}

/**
 * Enforces the invariants we promise the rest of the system: a quality score in
 * [1,10] and a non-empty complexity estimate. Throws on violation.
 */
export function validate(r: AIReviewFields): void {
  if (!Number.isInteger(r.qualityScore) || r.qualityScore < 1 || r.qualityScore > 10) {
    throw new Error(`qualityScore ${r.qualityScore} out of range 1-10`);
  }
  if (r.complexity === '') {
    throw new Error('complexity is empty');
  }
}

/** Maps a stored document to the shape served over GraphQL / the WebSocket. */
export function docToAIReview(doc: AIReviewDoc): AIReview {
  return {
    id: doc._id.toHexString(),
    snippetId: doc.snippetId,
    suggestions: doc.suggestions,
    complexity: doc.complexity,
    refactorHints: doc.refactorHints,
    securityFlags: doc.securityFlags,
    qualityScore: doc.qualityScore,
    language: doc.language,
    generatedAt: doc.generatedAt,
  };
}

/** Returns at most `max` string elements, ignoring anything that isn't a list. */
function clampStrings(value: unknown, max: number): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((v): v is string => typeof v === 'string').slice(0, max);
}
