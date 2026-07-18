import { GoogleGenAI } from '@google/genai';
import type { DBClient } from '../db/client.ts';
import type { Hub } from '../ws/hub.ts';
import { MessageType, newMessage } from '../ws/message.ts';
import { SYSTEM_PROMPT, buildUserPrompt } from './prompt.ts';
import { RateLimiter } from './rate-limiter.ts';
import {
  docToAIReview,
  toAIReviewFields,
  validate,
  type AIReview,
  type AIReviewFields,
  type GeminiReview,
} from './types.ts';

const MODEL_NAME = 'gemini-2.5-flash';

/** Bounds a single review pipeline run (including the retry's own call). */
const REVIEW_TIMEOUT_MS = 30_000;
/** Back-off before the single retry on a rate-limit error. */
const RETRY_DELAY_MS = 4_000;
/** Free tier allows 15 RPM; we self-limit to 10 RPM to stay comfortably under. */
const REQUESTS_PER_MINUTE = 10;

/**
 * Thrown when our local limiter rejects a call (we're pacing ourselves under
 * the free-tier quota). The frontend can surface this as "AI is busy, try again
 * shortly".
 */
export class RateLimitedError extends Error {
  constructor() {
    super('ai: rate limited, try again shortly');
    this.name = 'RateLimitedError';
  }
}

/**
 * The seam we mock in tests. The real implementation talks to Gemini; tests
 * inject a fake that returns canned JSON.
 */
export interface ContentGenerator {
  generate(prompt: string, signal: AbortSignal): Promise<string>;
}

export interface AIServiceOptions {
  generator: ContentGenerator;
  /** Omitted in tests, which skips persistence. */
  db?: DBClient | null;
  /** Omitted in tests, which skips the broadcast. */
  hub?: Hub | null;
  limiter?: RateLimiter;
  /** Overridable so tests don't have to sit through the real back-off. */
  retryDelayMs?: number;
}

/**
 * Orchestrates the review pipeline: rate-limit -> generate -> parse -> persist
 * -> broadcast.
 */
export class AIService {
  private readonly generator: ContentGenerator;
  private readonly db: DBClient | null;
  private readonly hub: Hub | null;
  private readonly limiter: RateLimiter;
  private readonly retryDelayMs: number;

  constructor(opts: AIServiceOptions) {
    this.generator = opts.generator;
    this.db = opts.db ?? null;
    this.hub = opts.hub ?? null;
    this.limiter = opts.limiter ?? new RateLimiter(60_000 / REQUESTS_PER_MINUTE, 1);
    this.retryDelayMs = opts.retryDelayMs ?? RETRY_DELAY_MS;
  }

  /**
   * Builds the production service: a Gemini client configured with the strict
   * system prompt and JSON response mode.
   */
  static create(apiKey: string, db: DBClient, hub: Hub): AIService {
    return new AIService({ generator: new GeminiGenerator(apiKey), db, hub });
  }

  /**
   * Runs the full pipeline for one snippet and returns the review.
   *
   * Steps: local rate-limit check -> Gemini call (30s budget, one retry on a
   * remote rate-limit) -> parse + validate -> save to ai_reviews -> broadcast
   * "ai_review_ready".
   */
  async reviewCode(snippetId: string, language: string, code: string): Promise<AIReview> {
    // Self-pace under the free-tier quota. We reject rather than wait so the
    // caller (and ultimately the frontend) gets a clear, fast signal.
    if (!this.limiter.allow()) throw new RateLimitedError();

    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), REVIEW_TIMEOUT_MS);

    let raw: string;
    try {
      raw = await this.generateWithRetry(buildUserPrompt(language, code), controller.signal);
    } catch (err) {
      throw new Error(`ai: generate: ${message(err)}`, { cause: err });
    } finally {
      clearTimeout(timeout);
    }

    let fields: AIReviewFields;
    try {
      fields = parseAndValidate(raw, snippetId, language);
    } catch (err) {
      throw new Error(`ai: ${message(err)}`, { cause: err });
    }

    let review: AIReview;
    try {
      review = await this.save(fields);
    } catch (err) {
      throw new Error(`ai: save: ${message(err)}`, { cause: err });
    }

    this.broadcast(review);
    return review;
  }

  /**
   * Calls the model once and, if it's a rate-limit error, waits out the
   * back-off and tries exactly one more time.
   */
  private async generateWithRetry(prompt: string, signal: AbortSignal): Promise<string> {
    try {
      return await this.generator.generate(prompt, signal);
    } catch (err) {
      if (!isRateLimit(err)) throw err;

      console.log(`ai: rate limited by Gemini, retrying once in ${this.retryDelayMs}ms`);
      await delay(this.retryDelayMs, signal);
      return this.generator.generate(prompt, signal);
    }
  }

  /** Upserts the review keyed by snippetId, so a re-run replaces the old one. */
  private async save(fields: AIReviewFields): Promise<AIReview> {
    if (!this.db) {
      // Test mode: no persistence, so there is no Mongo-assigned id yet.
      return { id: '', ...fields };
    }

    const doc = await this.db.aiReviews.findOneAndReplace({ snippetId: fields.snippetId }, fields, {
      upsert: true,
      returnDocument: 'after',
    });
    if (!doc) throw new Error('upsert returned no document');
    return docToAIReview(doc);
  }

  /** Notifies everyone viewing the snippet that the AI review is ready. */
  private broadcast(review: AIReview): void {
    if (!this.hub) return; // test mode
    this.hub.broadcastToRoom(
      review.snippetId,
      newMessage(MessageType.AIReviewReady, review.snippetId, review),
    );
  }
}

/**
 * Decodes the model's JSON into review fields and validates them. It
 * defensively strips code fences in case the model wraps the JSON despite our
 * instructions.
 */
function parseAndValidate(raw: string, snippetId: string, language: string): AIReviewFields {
  let parsed: GeminiReview;
  try {
    parsed = JSON.parse(stripCodeFences(raw)) as GeminiReview;
  } catch (err) {
    throw new Error(`invalid JSON from model: ${message(err)}`, { cause: err });
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('invalid JSON from model: expected an object');
  }

  const fields = toAIReviewFields(parsed, snippetId, language);
  validate(fields);
  return fields;
}

/** Removes a leading/trailing ```json ... ``` wrapper if present. */
function stripCodeFences(s: string): string {
  s = s.trim();
  if (!s.startsWith('```')) return s;

  // Drop the first line (``` or ```json) and a trailing ``` line.
  const nl = s.indexOf('\n');
  if (nl !== -1) s = s.slice(nl + 1);
  s = s.trim();
  if (s.endsWith('```')) s = s.slice(0, -3);
  return s.trim();
}

/** Reports whether err is a Gemini rate-limit / quota error. */
function isRateLimit(err: unknown): boolean {
  if (!err) return false;
  const status = (err as { status?: unknown }).status;
  if (status === 429) return true;

  const msg = message(err).toLowerCase();
  return (
    msg.includes('resource_exhausted') ||
    msg.includes('resourceexhausted') ||
    msg.includes('rate limit') ||
    msg.includes('quota') ||
    msg.includes('429')
  );
}

/** The production ContentGenerator, backed by the Gemini SDK. */
class GeminiGenerator implements ContentGenerator {
  private readonly client: GoogleGenAI;

  constructor(apiKey: string) {
    this.client = new GoogleGenAI({ apiKey });
  }

  async generate(prompt: string, signal: AbortSignal): Promise<string> {
    const response = await this.client.models.generateContent({
      model: MODEL_NAME,
      contents: prompt,
      config: {
        systemInstruction: SYSTEM_PROMPT,
        responseMimeType: 'application/json', // force JSON, not prose
        temperature: 0.2, // low temp: deterministic, analytical
        abortSignal: signal,
      },
    });

    const text = response.text;
    if (!text) {
      throw new Error('empty response from model (possibly blocked by safety filters)');
    }
    return text;
  }
}

/** Sleeps, rejecting early if the surrounding timeout fires. */
function delay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(new Error('aborted'));
      return;
    }
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    function onAbort(): void {
      clearTimeout(timer);
      reject(new Error('aborted'));
    }
    signal.addEventListener('abort', onAbort, { once: true });
  });
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
