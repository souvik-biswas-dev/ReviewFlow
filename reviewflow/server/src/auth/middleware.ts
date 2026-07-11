import type { NextFunction, Request, RequestHandler, Response } from 'express';
import { COOKIE_NAME, parseToken } from './jwt.ts';

/**
 * Reads the JWT from (in order):
 *   1. Authorization header ("Bearer <tok>") — used by fetch/GraphQL
 *   2. ?token= query param — used by WebSocket (browsers can't set WS headers)
 *   3. HttpOnly cookie — fallback for same-origin deployments
 */
export function extractToken(req: Request): string {
  const header = req.get('authorization');
  if (header && header.startsWith('Bearer ')) return header.slice(7);

  const query = req.query.token;
  if (typeof query === 'string' && query !== '') return query;

  const cookie = req.cookies?.[COOKIE_NAME] as string | undefined;
  return cookie ?? '';
}

/**
 * Validates the session JWT and, on success, stores the caller's identity on
 * the request. Any failure aborts with a 401 JSON error.
 *
 * It's written as a factory (returns the handler) rather than a bare handler so
 * the JWT secret can be injected from config instead of living in a module
 * global.
 */
export function authMiddleware(secret: string): RequestHandler {
  return (req: Request, res: Response, next: NextFunction) => {
    const token = extractToken(req);
    if (!token) {
      res.status(401).json({ error: 'missing authentication token' });
      return;
    }

    const claims = parseToken(secret, token);
    if (!claims) {
      res.status(401).json({ error: 'invalid or expired token' });
      return;
    }

    req.userId = claims.userId;
    req.githubUsername = claims.githubUsername;
    next();
  };
}

/**
 * A *soft* counterpart to authMiddleware for the /graphql endpoint: if a valid
 * session token is present it attaches the identity, but it never rejects —
 * resolvers decide which operations require authentication.
 */
export function graphqlContext(secret: string): RequestHandler {
  return (req: Request, _res: Response, next: NextFunction) => {
    const token = extractToken(req);
    if (token) {
      const claims = parseToken(secret, token);
      if (claims) {
        req.userId = claims.userId;
        req.githubUsername = claims.githubUsername;
      }
    }
    next();
  };
}
