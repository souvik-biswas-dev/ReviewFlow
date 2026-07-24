import cookieParser from 'cookie-parser';
import cors from 'cors';
import express, { type Express, type NextFunction, type Request, type Response } from 'express';
import { createHandler } from 'graphql-http/lib/use/express';
import type { AIService } from './ai/service.ts';
import { authRouter } from './auth/routes.ts';
import { graphqlContext } from './auth/middleware.ts';
import type { Config } from './config.ts';
import type { DBClient } from './db/client.ts';
import { playgroundHTML } from './graphql/playground.ts';
import { buildSchema } from './graphql/schema.ts';
import type { GraphQLContext } from './graphql/types.ts';
import { notificationsRouter } from './notifications/routes.ts';
import type { Hub } from './ws/hub.ts';

/**
 * Snippets are posted as JSON, so the body limit has to comfortably fit a large
 * source file plus its previous version.
 */
const BODY_LIMIT = '5mb';

/**
 * Builds the Express app with global middleware and all routes wired up. The
 * hub and (optional) AI service are created in index.ts so it can own their
 * lifecycle.
 */
export function buildApp(cfg: Config, db: DBClient, hub: Hub, ai: AIService | null): Express {
  const app = express();

  app.disable('x-powered-by');
  app.set('trust proxy', true); // Caddy terminates TLS in front of us

  app.use(requestLogger);

  // CORS: the SvelteKit dev server (default :5173) calls this API from the
  // browser. credentials is required so the session cookie is sent, and it
  // forbids the "*" wildcard — hence an explicit origin.
  app.use(
    cors({
      origin: cfg.corsOrigin,
      methods: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'],
      allowedHeaders: ['Origin', 'Content-Type', 'Authorization'],
      exposedHeaders: ['Content-Length'],
      credentials: true,
      maxAge: 12 * 60 * 60, // seconds
    }),
  );

  app.use(express.json({ limit: BODY_LIMIT }));
  app.use(cookieParser());

  registerHealth(app, db);
  app.use('/auth', authRouter(cfg, db));
  registerGraphQL(app, cfg, db, hub, ai);
  app.use('/notifications', notificationsRouter(cfg, db));

  app.use(notFound);
  app.use(errorHandler);

  return app;
}

/**
 * GET (and, via Express's automatic HEAD routing, HEAD) /health with a live DB
 * ping. UptimeRobot and many other monitors default to HEAD requests.
 */
function registerHealth(app: Express, db: DBClient): void {
  app.get('/health', async (_req: Request, res: Response) => {
    try {
      await db.ping(2000);
    } catch {
      res.status(503).json({ status: 'degraded', db: 'disconnected' });
      return;
    }
    res.json({ status: 'ok', db: 'connected' });
  });
}

/** Wires the GraphQL execution endpoint and the playground. */
function registerGraphQL(
  app: Express,
  cfg: Config,
  db: DBClient,
  hub: Hub,
  ai: AIService | null,
): void {
  const schema = buildSchema();

  // graphqlContext is soft auth: it attaches the user id when a valid token is
  // present, but never rejects — resolvers decide which operations require
  // authentication.
  app.post(
    '/graphql',
    graphqlContext(cfg.jwtSecret),
    createHandler({
      schema,
      context: (req): GraphQLContext => ({
        db,
        hub,
        ai,
        userId: (req.raw as Request).userId,
      }),
    }),
  );

  // Interactive query explorer, served at GET /graphql/playground.
  app.get('/graphql/playground', (_req: Request, res: Response) => {
    res.type('html').send(playgroundHTML('/graphql'));
  });
}

/**
 * One-line access log, in the spirit of gin.Logger(). Hand-rolled rather than
 * pulling in morgan: it's a dozen lines and keeps the dependency surface small.
 */
function requestLogger(req: Request, res: Response, next: NextFunction): void {
  const start = process.hrtime.bigint();
  res.on('finish', () => {
    const ms = Number(process.hrtime.bigint() - start) / 1e6;
    console.log(
      `${new Date().toISOString()} | ${res.statusCode} | ${ms.toFixed(1)}ms | ${req.method} ${req.originalUrl}`,
    );
  });
  next();
}

function notFound(_req: Request, res: Response): void {
  res.status(404).json({ error: 'not found' });
}

/**
 * Last line of defence, equivalent to gin.Recovery(): Express 5 forwards both
 * thrown and rejected handler errors here, so a bug returns a 500 instead of
 * killing the process.
 */
function errorHandler(err: unknown, _req: Request, res: Response, _next: NextFunction): void {
  console.error('server: unhandled error:', err);
  if (res.headersSent) return;

  // A malformed JSON body is the client's fault, not ours.
  const status = (err as { status?: number; statusCode?: number })?.status ?? 500;
  if (status >= 400 && status < 500) {
    res.status(status).json({ error: 'invalid request' });
    return;
  }
  res.status(500).json({ error: 'internal server error' });
}
