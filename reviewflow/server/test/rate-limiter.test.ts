import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { RateLimiter } from '../src/ai/rate-limiter.ts';

/**
 * The token bucket replaces Go's golang.org/x/time/rate, so it carries its own
 * tests. A fake clock keeps them instant and deterministic.
 */
function fakeClock(): { now: () => number; advance: (ms: number) => void } {
  let t = 1_000_000;
  return {
    now: () => t,
    advance: (ms: number) => {
      t += ms;
    },
  };
}

describe('RateLimiter', () => {
  it('allows up to the burst, then denies', () => {
    const clock = fakeClock();
    const limiter = new RateLimiter(6_000, 2, clock.now);

    assert.equal(limiter.allow(), true);
    assert.equal(limiter.allow(), true);
    assert.equal(limiter.allow(), false);
  });

  it('refills one token per interval', () => {
    const clock = fakeClock();
    const limiter = new RateLimiter(6_000, 1, clock.now);

    assert.equal(limiter.allow(), true);
    assert.equal(limiter.allow(), false);

    clock.advance(5_999);
    assert.equal(limiter.allow(), false, 'just short of a full token');

    clock.advance(1);
    assert.equal(limiter.allow(), true);
  });

  it('never accrues more than the burst', () => {
    const clock = fakeClock();
    const limiter = new RateLimiter(6_000, 1, clock.now);

    clock.advance(60 * 60 * 1000); // an hour of idling
    assert.equal(limiter.allow(), true);
    assert.equal(limiter.allow(), false, 'capacity is still 1');
  });

  it('unlimited() always allows and denyAll() always denies', () => {
    const unlimited = RateLimiter.unlimited();
    for (let i = 0; i < 100; i++) assert.equal(unlimited.allow(), true);

    const denied = RateLimiter.denyAll();
    assert.equal(denied.allow(), false);
  });
});
