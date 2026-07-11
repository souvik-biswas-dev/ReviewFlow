import { randomBytes } from 'node:crypto';
import { Router, type CookieOptions, type Request, type Response } from 'express';
import { ObjectId } from 'mongodb';
import { isProduction, type Config } from '../config.ts';
import type { DBClient } from '../db/client.ts';
import type { UserDoc } from '../db/models.ts';
import { authMiddleware } from './middleware.ts';
import { COOKIE_NAME, TOKEN_TTL_SECONDS, generateToken } from './jwt.ts';

// GitHub OAuth endpoints and the short-lived cookie used for CSRF protection.
const GITHUB_AUTHORIZE_URL = 'https://github.com/login/oauth/authorize';
const GITHUB_TOKEN_URL = 'https://github.com/login/oauth/access_token';
const GITHUB_USER_URL = 'https://api.github.com/user';
const OAUTH_STATE_COOKIE = 'rf_oauth_state';

// Budget for a single call to GitHub, so a hung request can't pin a socket
// forever (the Go version set this on a shared http.Client).
const GITHUB_TIMEOUT_MS = 10_000;

/** The subset of GitHub's /user response we care about. */
interface GitHubProfile {
  id: number;
  login: string;
  avatar_url: string;
}

/** Wires the GitHub OAuth + session endpoints onto a router. */
export function authRouter(cfg: Config, db: DBClient): Router {
  const router = Router();

  router.get('/github', githubLogin(cfg));
  router.get('/github/callback', githubCallback(cfg, db));
  // /auth/me requires a valid session token.
  router.get('/me', authMiddleware(cfg.jwtSecret), me(db));

  return router;
}

/**
 * GET /auth/github — kicks off the OAuth flow by redirecting the browser to
 * GitHub's authorize page with a random anti-CSRF state value.
 */
function githubLogin(cfg: Config) {
  return (_req: Request, res: Response): void => {
    const state = randomBytes(32).toString('hex');

    // Remember the state in a short-lived HttpOnly cookie so we can verify, on
    // the callback, that the response corresponds to a request we initiated.
    res.cookie(OAUTH_STATE_COOKIE, state, {
      ...cookieSecurity(cfg),
      path: '/',
      maxAge: 600_000, // 10 minutes
      httpOnly: true,
    });

    const q = new URLSearchParams({
      client_id: cfg.githubClientID,
      redirect_uri: cfg.githubRedirectURL,
      scope: 'read:user', // we only need the public profile
      state,
    });
    res.redirect(307, `${GITHUB_AUTHORIZE_URL}?${q.toString()}`);
  };
}

/**
 * GET /auth/github/callback — completes the flow: verify state, exchange the
 * code for a GitHub token, fetch the profile, upsert the user, and issue our
 * own session JWT as an HttpOnly cookie before redirecting back to the frontend.
 */
function githubCallback(cfg: Config, db: DBClient) {
  return async (req: Request, res: Response): Promise<void> => {
    // 1. CSRF check: the state echoed by GitHub must match our cookie.
    const stateCookie = req.cookies?.[OAUTH_STATE_COOKIE] as string | undefined;
    if (!stateCookie || req.query.state !== stateCookie) {
      res.status(400).json({ error: 'invalid oauth state' });
      return;
    }
    res.clearCookie(OAUTH_STATE_COOKIE, { path: '/' }); // consume it

    const code = req.query.code;
    if (typeof code !== 'string' || code === '') {
      res.status(400).json({ error: 'missing authorization code' });
      return;
    }

    // 2. Exchange the code for a GitHub access token.
    let ghToken: string;
    try {
      ghToken = await exchangeCode(cfg, code);
    } catch (err) {
      console.error('auth: github token exchange failed:', err);
      res.status(502).json({ error: 'github token exchange failed' });
      return;
    }

    // 3. Fetch the GitHub profile (id, login, avatar_url).
    let profile: GitHubProfile;
    try {
      profile = await fetchGitHubUser(ghToken);
    } catch (err) {
      console.error('auth: failed to fetch github profile:', err);
      res.status(502).json({ error: 'failed to fetch github profile' });
      return;
    }

    // 4. Upsert the user (insert on first login, refresh username/avatar after).
    let user: UserDoc;
    try {
      user = await upsertUser(db, profile);
    } catch (err) {
      console.error('auth: failed to persist user:', err);
      res.status(500).json({ error: 'failed to persist user' });
      return;
    }

    // 5. Issue our session JWT in an HttpOnly cookie.
    const token = generateToken(cfg.jwtSecret, user._id.toHexString(), user.githubUsername);
    setSessionCookie(res, cfg, token);

    // 6. Hand the browser back to the SvelteKit app.
    // Also pass the token as a query param so the frontend can store it in
    // localStorage — required when frontend and backend are on different
    // domains (cross-origin cookies are blocked by Chrome's Privacy Sandbox).
    res.redirect(307, `${cfg.frontendURL}/auth/callback?token=${encodeURIComponent(token)}`);
  };
}

/** GET /auth/me (behind authMiddleware) — returns the current user. */
function me(db: DBClient) {
  return async (req: Request, res: Response): Promise<void> => {
    let oid: ObjectId;
    try {
      oid = new ObjectId(req.userId);
    } catch {
      res.status(401).json({ error: 'invalid user id in token' });
      return;
    }

    const user = await db.users.findOne({ _id: oid });
    if (!user) {
      res.status(404).json({ error: 'user not found' });
      return;
    }

    res.json({
      id: user._id.toHexString(),
      githubUsername: user.githubUsername,
      avatarUrl: user.avatarUrl,
      createdAt: user.createdAt,
    });
  };
}

// --- internal helpers ---

/** Swaps the OAuth `code` for a GitHub access token. */
async function exchangeCode(cfg: Config, code: string): Promise<string> {
  const form = new URLSearchParams({
    client_id: cfg.githubClientID,
    client_secret: cfg.githubClientSecret,
    code,
    redirect_uri: cfg.githubRedirectURL,
  });

  const resp = await fetch(GITHUB_TOKEN_URL, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
      Accept: 'application/json', // ask for JSON, not form-encoded
    },
    body: form,
    signal: AbortSignal.timeout(GITHUB_TIMEOUT_MS),
  });

  const body = (await resp.json()) as {
    access_token?: string;
    error?: string;
    error_description?: string;
  };
  if (body.error) {
    throw new Error(`github oauth: ${body.error}: ${body.error_description ?? ''}`);
  }
  if (!body.access_token) {
    throw new Error('github oauth: empty access token');
  }
  return body.access_token;
}

/** Reads the authenticated user's public profile. */
async function fetchGitHubUser(token: string): Promise<GitHubProfile> {
  const resp = await fetch(GITHUB_USER_URL, {
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: 'application/vnd.github+json',
    },
    signal: AbortSignal.timeout(GITHUB_TIMEOUT_MS),
  });
  if (!resp.ok) {
    throw new Error(`github user: unexpected status ${resp.status}`);
  }
  return (await resp.json()) as GitHubProfile;
}

/**
 * Inserts the user on first login and otherwise refreshes the mutable profile
 * fields, keying on the immutable GitHub numeric id.
 */
async function upsertUser(db: DBClient, p: GitHubProfile): Promise<UserDoc> {
  const now = new Date();

  const user = await db.users.findOneAndUpdate(
    { githubId: p.id },
    {
      $set: {
        githubUsername: p.login,
        avatarUrl: p.avatar_url,
        updatedAt: now,
      },
      $setOnInsert: {
        githubId: p.id,
        createdAt: now,
      },
    },
    { upsert: true, returnDocument: 'after' }, // return the document *after* the upsert
  );

  if (!user) throw new Error('upsert user: no document returned');
  return user;
}

/**
 * Shared cookie security flags. In production the frontend and backend may be
 * on different domains, so we need SameSite=None + Secure for the browser to
 * send cookies on cross-origin requests. In development we use SameSite=Lax so
 * localhost auth works without TLS.
 */
function cookieSecurity(cfg: Config): Pick<CookieOptions, 'sameSite' | 'secure'> {
  const secure = isProduction(cfg);
  return { sameSite: secure ? 'none' : 'lax', secure };
}

/** Writes the JWT as an HttpOnly cookie. */
function setSessionCookie(res: Response, cfg: Config, token: string): void {
  res.cookie(COOKIE_NAME, token, {
    ...cookieSecurity(cfg),
    path: '/',
    maxAge: TOKEN_TTL_SECONDS * 1000,
    httpOnly: true,
  });
}
