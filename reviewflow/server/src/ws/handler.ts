import type { IncomingMessage, Server } from 'node:http';
import type { Duplex } from 'node:stream';
import { WebSocketServer } from 'ws';
import { COOKIE_NAME, parseToken } from '../auth/jwt.ts';
import { Client, MAX_MESSAGE_BYTES } from './client.ts';
import type { Hub } from './hub.ts';

/** Only /ws/<snippetId> upgrades; anything else gets a 404. */
const WS_PATH = /^\/ws\/([^/?#]+)$/;

/**
 * Attaches the WebSocket endpoint to the HTTP server.
 *
 * Authentication and the origin check happen *before* the upgrade: once a
 * connection is upgraded we can no longer write a normal HTTP error, so every
 * rejection is answered with a plain HTTP status on the raw socket.
 *
 * @param allowedOrigin the browser origin permitted to open sockets (the
 *   SvelteKit app, e.g. http://localhost:5173).
 */
export function attachWebSocket(
  server: Server,
  hub: Hub,
  jwtSecret: string,
  allowedOrigin: string,
): WebSocketServer {
  const wss = new WebSocketServer({ noServer: true, maxPayload: MAX_MESSAGE_BYTES });

  server.on('upgrade', (req: IncomingMessage, socket: Duplex, head: Buffer) => {
    const url = new URL(req.url ?? '/', 'http://localhost');
    const match = WS_PATH.exec(url.pathname);
    if (!match?.[1]) {
      reject(socket, 404, 'Not Found');
      return;
    }
    const snippetId = decodeURIComponent(match[1]);

    // Reject cross-origin upgrades. The browser sends Origin; only our own
    // frontend may connect. (Auth is also enforced, below.)
    const origin = req.headers.origin;
    if (origin !== allowedOrigin) {
      console.log(`ws: rejected upgrade from origin ${JSON.stringify(origin ?? '')}`);
      reject(socket, 403, 'Forbidden');
      return;
    }

    const claims = parseToken(jwtSecret, extractUpgradeToken(req, url));
    if (!claims) {
      reject(socket, 401, 'Unauthorized');
      return;
    }

    wss.handleUpgrade(req, socket, head, (ws) => {
      const client = new Client(hub, ws, snippetId, claims.userId, claims.githubUsername);

      // Register before wiring the socket's handlers so the join broadcast and
      // presence_list are produced against a consistent room membership.
      hub.register(client);
      client.start();
    });
  });

  return wss;
}

/**
 * Same precedence as the HTTP middleware: Authorization header, then ?token=
 * (what browsers must use, since they can't set headers on a WebSocket), then
 * the session cookie.
 */
function extractUpgradeToken(req: IncomingMessage, url: URL): string {
  const header = req.headers.authorization;
  if (header?.startsWith('Bearer ')) return header.slice(7);

  const query = url.searchParams.get('token');
  if (query) return query;

  return readCookie(req.headers.cookie, COOKIE_NAME);
}

/** Minimal Cookie-header parser — the upgrade path never runs Express middleware. */
function readCookie(header: string | undefined, name: string): string {
  if (!header) return '';
  for (const part of header.split(';')) {
    const eq = part.indexOf('=');
    if (eq === -1) continue;
    if (part.slice(0, eq).trim() === name) {
      return decodeURIComponent(part.slice(eq + 1).trim());
    }
  }
  return '';
}

/** Answers a pre-upgrade rejection with a plain HTTP response and hangs up. */
function reject(socket: Duplex, status: number, reason: string): void {
  socket.write(`HTTP/1.1 ${status} ${reason}\r\nConnection: close\r\n\r\n`);
  socket.destroy();
}
