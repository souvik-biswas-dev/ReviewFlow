/**
 * A token bucket, standing in for Go's golang.org/x/time/rate.Limiter.
 *
 * Tokens accrue continuously at one per `refillIntervalMs` up to `burst`, and
 * `allow()` consumes one without ever blocking — a rejected call is meant to
 * fail fast rather than queue.
 */
export class RateLimiter {
  private readonly refillIntervalMs: number;
  private readonly burst: number;
  private readonly now: () => number;

  private tokens: number;
  private last: number;

  /**
   * @param refillIntervalMs milliseconds per token; 0 means unlimited.
   * @param burst bucket capacity; 0 denies every call.
   */
  constructor(refillIntervalMs: number, burst: number, now: () => number = Date.now) {
    this.refillIntervalMs = refillIntervalMs;
    this.burst = burst;
    this.now = now;
    this.tokens = burst;
    this.last = now();
  }

  /** A limiter that never rejects (used in tests). */
  static unlimited(): RateLimiter {
    return new RateLimiter(0, Number.POSITIVE_INFINITY);
  }

  /** A limiter that always rejects (used in tests). */
  static denyAll(): RateLimiter {
    return new RateLimiter(0, 0);
  }

  /** Consumes a token if one is available. Never blocks. */
  allow(): boolean {
    if (this.burst <= 0) return false;

    const now = this.now();
    if (this.refillIntervalMs > 0) {
      const accrued = (now - this.last) / this.refillIntervalMs;
      if (accrued > 0) {
        this.tokens = Math.min(this.burst, this.tokens + accrued);
        this.last = now;
      }
    } else {
      this.tokens = this.burst; // unlimited: the bucket is always full
    }

    if (this.tokens < 1) return false;
    this.tokens -= 1;
    return true;
  }
}
