import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { RateLimiter } from '../src/ai/rate-limiter.ts';
import { AIService, RateLimitedError, type ContentGenerator } from '../src/ai/service.ts';
import { MAX_REFACTOR_HINTS, MAX_SUGGESTIONS } from '../src/ai/types.ts';

/**
 * A ContentGenerator that returns canned output, standing in for the real
 * Gemini call so the parsing/validation pipeline can be tested offline.
 */
function fakeGen(out: string, err?: Error): ContentGenerator {
  return {
    generate: async () => {
      if (err) throw err;
      return out;
    },
  };
}

/**
 * Builds an AIService with the given generator, an unlimited limiter, and no
 * db/hub (so save + broadcast are skipped).
 */
function newTestService(generator: ContentGenerator): AIService {
  return new AIService({ generator, limiter: RateLimiter.unlimited() });
}

const VALID_JSON = `{
  "suggestions": ["use errors.Is for sentinel checks", "handle io.EOF explicitly", "pass a context with timeout"],
  "complexity": "O(n) time, O(1) space",
  "refactorHints": ["extract the parse loop into its own function", "rename x to itemCount"],
  "securityFlags": [],
  "qualityScore": 8
}`;

describe('AIService.reviewCode', () => {
  it('parses a valid response', async () => {
    const svc = newTestService(fakeGen(VALID_JSON));

    const got = await svc.reviewCode('snip1', 'go', 'package main');

    assert.equal(got.qualityScore, 8);
    assert.equal(got.complexity, 'O(n) time, O(1) space');
    assert.equal(got.suggestions.length, 3);
    assert.equal(got.refactorHints.length, 2);
    // Metadata is set by us, not the model.
    assert.equal(got.snippetId, 'snip1');
    assert.equal(got.language, 'go');
    assert.ok(got.generatedAt instanceof Date && !Number.isNaN(got.generatedAt.getTime()));
    // Non-null list invariant.
    assert.deepEqual(got.securityFlags, []);
  });

  it('strips markdown code fences', async () => {
    const svc = newTestService(fakeGen('```json\n' + VALID_JSON + '\n```'));

    const got = await svc.reviewCode('snip2', 'go', 'code');

    assert.equal(got.qualityScore, 8);
  });

  it('clamps lists and fills missing ones', async () => {
    // 7 suggestions (cap 5), 4 refactorHints (cap 3), securityFlags omitted.
    const overflow = `{
      "suggestions": ["a","b","c","d","e","f","g"],
      "complexity": "O(1) time, O(1) space",
      "refactorHints": ["r1","r2","r3","r4"],
      "qualityScore": 5
    }`;
    const svc = newTestService(fakeGen(overflow));

    const got = await svc.reviewCode('snip3', 'python', 'code');

    assert.equal(got.suggestions.length, MAX_SUGGESTIONS);
    assert.equal(got.refactorHints.length, MAX_REFACTOR_HINTS);
    assert.deepEqual(got.securityFlags, [], 'omitted securityFlags should normalize to []');
  });

  it('rejects malformed JSON', async () => {
    const svc = newTestService(fakeGen('this is not json'));

    await assert.rejects(() => svc.reviewCode('snip4', 'go', 'code'), /invalid JSON from model/);
  });

  it('rejects an out-of-range quality score', async () => {
    const bad = `{"suggestions":[],"complexity":"O(1)","refactorHints":[],"securityFlags":[],"qualityScore":42}`;
    const svc = newTestService(fakeGen(bad));

    await assert.rejects(() => svc.reviewCode('snip5', 'go', 'code'), /out of range 1-10/);
  });

  it('rejects an empty complexity estimate', async () => {
    const bad = `{"suggestions":[],"refactorHints":[],"securityFlags":[],"qualityScore":7}`;
    const svc = newTestService(fakeGen(bad));

    await assert.rejects(() => svc.reviewCode('snip6', 'go', 'code'), /complexity is empty/);
  });

  it('returns RateLimitedError when the limiter is exhausted', async () => {
    const svc = new AIService({ generator: fakeGen(VALID_JSON), limiter: RateLimiter.denyAll() });

    await assert.rejects(
      () => svc.reviewCode('snip7', 'go', 'code'),
      (err: unknown) => err instanceof RateLimitedError,
    );
  });

  it('retries exactly once after a remote rate-limit error', async () => {
    let calls = 0;
    const generator: ContentGenerator = {
      generate: async () => {
        calls += 1;
        if (calls === 1) throw Object.assign(new Error('quota exceeded'), { status: 429 });
        return VALID_JSON;
      },
    };
    const svc = new AIService({ generator, limiter: RateLimiter.unlimited(), retryDelayMs: 0 });

    const got = await svc.reviewCode('snip8', 'go', 'code');

    assert.equal(calls, 2, 'should call the model twice: original + one retry');
    assert.equal(got.qualityScore, 8);
  });

  it('does not retry a non-rate-limit error', async () => {
    let calls = 0;
    const generator: ContentGenerator = {
      generate: async () => {
        calls += 1;
        throw new Error('bad request');
      },
    };
    const svc = new AIService({ generator, limiter: RateLimiter.unlimited() });

    await assert.rejects(() => svc.reviewCode('snip9', 'go', 'code'), /bad request/);
    assert.equal(calls, 1);
  });
});
