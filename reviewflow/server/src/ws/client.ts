import type { RawData, WebSocket } from 'ws';
import type { Hub } from './hub.ts';
import { MessageType, newMessage, type Message } from './message.ts';

// Timing / sizing constants for a single connection.

/** Keepalive timeout: if no readable frame (data or pong) arrives within this
 *  window, the connection is dropped. (Spec: 60s.) */
const PONG_WAIT_MS = 60_000;
/** Must be < PONG_WAIT_MS so a ping/pong round-trip can complete before the
 *  deadline. 90% of the deadline is the usual choice. */
const PING_PERIOD_MS = (PONG_WAIT_MS * 9) / 10;
/** Caps inbound frames. Clients only ever send small control messages (typing /
 *  ping); large code arrives via GraphQL, not the socket. */
export const MAX_MESSAGE_BYTES = 64 * 1024;
/**
 * Outbound backlog watermark. The Go version buffered 256 *messages* per client
 * in a channel; `ws` queues bytes on the socket instead, so the equivalent
 * "this peer can't keep up" signal is how much is still unflushed.
 */
const MAX_BUFFERED_BYTES = 1024 * 1024;

/**
 * One WebSocket connection. Its identity fields are immutable after creation;
 * all coordination with other clients goes through the hub.
 */
export class Client {
  readonly snippetId: string;
  readonly userId: string;
  readonly username: string;

  private readonly hub: Hub;
  private readonly socket: WebSocket;

  /** Set once the hub has closed us, so close() and eviction stay idempotent. */
  private closed = false;
  private pingTimer: NodeJS.Timeout | null = null;
  private deadlineTimer: NodeJS.Timeout | null = null;

  constructor(hub: Hub, socket: WebSocket, snippetId: string, userId: string, username: string) {
    this.hub = hub;
    this.socket = socket;
    this.snippetId = snippetId;
    this.userId = userId;
    this.username = username;
  }

  /**
   * Wires up the socket's event handlers and starts the keepalive timers. Call
   * after the hub has registered the client, so the presence_list queued during
   * registration is already on the socket.
   */
  start(): void {
    this.socket.on('message', (raw: RawData, isBinary: boolean) => {
      // Any inbound frame counts as liveness.
      this.resetDeadline();
      if (isBinary) return; // we only speak JSON text frames
      this.handleInbound(raw.toString());
    });

    // A pong control frame (auto-sent by browsers in response to our ping)
    // resets the deadline — this is the primary keepalive.
    this.socket.on('pong', () => this.resetDeadline());

    this.socket.on('close', () => this.hub.unregister(this));
    this.socket.on('error', (err: Error) => {
      // Normal disconnects surface as 'close'; anything reaching here is worth
      // a line in the log before the 'close' handler unregisters us.
      console.error(`ws: socket error (user=${this.userId} snippet=${this.snippetId}):`, err.message);
    });

    this.pingTimer = setInterval(() => {
      if (this.socket.readyState !== this.socket.OPEN) return;
      this.socket.ping();
    }, PING_PERIOD_MS);

    this.resetDeadline();
  }

  /**
   * Non-blocking send. Returns false when the peer is gone or its backlog is
   * over the watermark, which the hub treats as "evict this slow consumer".
   */
  trySend(data: string): boolean {
    if (this.closed || this.socket.readyState !== this.socket.OPEN) return false;
    if (this.socket.bufferedAmount > MAX_BUFFERED_BYTES) return false;
    this.socket.send(data);
    return true;
  }

  /** Stops the timers and closes the socket. Safe to call more than once. */
  close(): void {
    if (this.closed) return;
    this.closed = true;

    if (this.pingTimer) clearInterval(this.pingTimer);
    if (this.deadlineTimer) clearTimeout(this.deadlineTimer);
    this.pingTimer = null;
    this.deadlineTimer = null;

    if (this.socket.readyState === this.socket.OPEN) {
      this.socket.close(1000, ''); // 1000 = normal closure
    } else {
      this.socket.terminate();
    }
  }

  /**
   * Restarts the read deadline. If nothing readable arrives within PONG_WAIT_MS
   * the peer is considered gone and the connection is torn down.
   */
  private resetDeadline(): void {
    if (this.closed) return;
    if (this.deadlineTimer) clearTimeout(this.deadlineTimer);
    this.deadlineTimer = setTimeout(() => {
      console.log(`ws: keepalive timeout (user=${this.userId} snippet=${this.snippetId})`);
      this.socket.terminate(); // 'close' fires -> hub.unregister
    }, PONG_WAIT_MS);
  }

  /**
   * Parses a client frame and acts on the message types a client is allowed to
   * originate. Server-authoritative types (review_added, presence_*,
   * ai_review_ready) are ignored when sent by a client.
   */
  private handleInbound(raw: string): void {
    let incoming: Message;
    try {
      incoming = JSON.parse(raw) as Message;
    } catch {
      return; // ignore malformed frames
    }

    switch (incoming?.type) {
      case MessageType.Ping: {
        // App-level heartbeat. Browser JS can't send WS ping frames, so this is
        // how a web client proves liveness; we reply straight to its own socket
        // (non-blocking — if even that fails, the hub will evict it).
        const pong = newMessage(MessageType.Pong, this.snippetId);
        this.trySend(JSON.stringify(pong));
        break;
      }

      case MessageType.Typing: {
        // Relay "typing" to the rest of the room. We re-stamp the sender from
        // the authenticated identity so a client can't impersonate anyone.
        const out: Message = {
          type: MessageType.Typing,
          snippetId: this.snippetId,
          payload: incoming.payload,
          senderId: this.userId,
          senderUsername: this.username,
          timestamp: new Date().toISOString(),
        };
        this.hub.relay(this, JSON.stringify(out));
        break;
      }

      default:
        // Ignore everything else a client might try to send.
        break;
    }
  }
}
