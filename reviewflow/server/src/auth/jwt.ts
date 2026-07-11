import jwt from 'jsonwebtoken';

/** Session lifetime. Spec: tokens expire after 7 days. */
export const TOKEN_TTL_SECONDS = 7 * 24 * 60 * 60;

/** The HttpOnly cookie that carries the session JWT. */
export const COOKIE_NAME = 'rf_token';

/**
 * The JWT payload. The custom fields sit alongside the registered claims, so
 * the encoded payload is exactly { userId, githubUsername, exp, iat }.
 */
export interface Claims {
  userId: string;
  githubUsername: string;
  exp: number;
  iat: number;
}

/** Signs a 7-day HS256 JWT for the given user. */
export function generateToken(secret: string, userId: string, githubUsername: string): string {
  return jwt.sign({ userId, githubUsername }, secret, {
    algorithm: 'HS256',
    expiresIn: TOKEN_TTL_SECONDS,
  });
}

/**
 * Validates the signature and expiry and returns the claims, or null when the
 * token is missing, malformed, expired or not signed by us.
 *
 * The algorithm is pinned to HS256: anything else (e.g. "none" or an RS256
 * token) is rejected, defending against algorithm-confusion attacks where an
 * attacker swaps the alg header to bypass signature verification.
 */
export function parseToken(secret: string, token: string): Claims | null {
  try {
    const decoded = jwt.verify(token, secret, { algorithms: ['HS256'] });
    if (typeof decoded === 'string') return null;

    const { userId, githubUsername, exp, iat } = decoded as Partial<Claims>;
    if (typeof userId !== 'string' || userId === '') return null;

    return {
      userId,
      githubUsername: typeof githubUsername === 'string' ? githubUsername : '',
      exp: exp ?? 0,
      iat: iat ?? 0,
    };
  } catch {
    return null;
  }
}
