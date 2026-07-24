import { createServer } from 'node:http';
import { buildApp } from './app.ts';
import { AIService } from './ai/service.ts';
import { loadConfig } from './config.ts';
import { connect, type DBClient } from './db/client.ts';
import { ensureIndexes } from './db/models.ts';
import { Hub } from './ws/hub.ts';
import { attachWebSocket } from './ws/handler.ts';

/** How long in-flight requests get to finish before we force the exit. */
const SHUTDOWN_GRACE_MS = 10_000;
/** Budget for the startup index creation pass. */
const INDEX_TIMEOUT_MS = 10_000;

async function main(): Promise<void> {
  // 1. Configuration -------------------------------------------------------
  const cfg = loadConfig();

  // 2. Database ------------------------------------------------------------
  // Fail fast: a server that can't reach MongoDB shouldn't accept traffic.
  let db: DBClient;
  try {
    db = await connect(cfg.mongoURI, cfg.mongoDB);
  } catch (err) {
    console.error('startup:', err);
    process.exit(1);
  }

  // Create required indexes before serving. Idempotent, so safe every boot.
  try {
    await withTimeout(ensureIndexes(db.database), INDEX_TIMEOUT_MS, 'ensure indexes');
  } catch (err) {
    console.error('startup: ensure indexes:', err);
    process.exit(1);
  }

  // 3. Real-time hub + AI service -----------------------------------------
  const hub = new Hub();

  // AI is optional: with no API key the app runs fine, snippets just never get
  // an AI review (aiReview stays null).
  const ai = cfg.geminiAPIKey ? AIService.create(cfg.geminiAPIKey, db, hub) : null;

  // 4. HTTP + WebSocket server ---------------------------------------------
  const server = createServer(buildApp(cfg, db, hub, ai));
  // Guards against slow-loris style stalls on the request headers.
  server.headersTimeout = 5_000;

  attachWebSocket(server, hub, cfg.jwtSecret, cfg.corsOrigin);

  server.listen(cfg.port, () => {
    console.log(`server: listening on :${cfg.port} (env=${cfg.environment})`);
  });

  server.on('error', (err) => {
    console.error('server:', err);
    process.exit(1);
  });

  // 5. Graceful shutdown ---------------------------------------------------
  let shuttingDown = false;
  const shutdown = async (signal: string): Promise<void> => {
    if (shuttingDown) return;
    shuttingDown = true;
    console.log(`server: shutdown signal received (${signal})`);

    // Hard deadline: if anything refuses to settle, exit anyway.
    const force = setTimeout(() => {
      console.error('server: forced shutdown');
      process.exit(1);
    }, SHUTDOWN_GRACE_MS);
    force.unref();

    // Stop accepting new connections, then drop the live sockets — an open
    // WebSocket would otherwise hold the server open until it times out.
    await new Promise<void>((resolve) => server.close(() => resolve()));
    hub.closeAll();

    // Close DB connections last, once the server is no longer serving requests.
    try {
      await db.disconnect();
    } catch (err) {
      console.error('db: disconnect error:', err);
    }

    console.log('server: stopped cleanly');
    process.exit(0);
  };

  process.on('SIGINT', () => void shutdown('SIGINT'));
  process.on('SIGTERM', () => void shutdown('SIGTERM'));

  // A rejected promise that reaches the top level is a bug, but it should not
  // take the process down mid-request.
  process.on('unhandledRejection', (reason) => {
    console.error('server: unhandled rejection:', reason);
  });
}

/** Rejects if `promise` hasn't settled within ms. */
async function withTimeout<T>(promise: Promise<T>, ms: number, label: string): Promise<T> {
  let timer: NodeJS.Timeout | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_resolve, reject) => {
        timer = setTimeout(() => reject(new Error(`${label}: timed out after ${ms}ms`)), ms);
      }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

await main();
