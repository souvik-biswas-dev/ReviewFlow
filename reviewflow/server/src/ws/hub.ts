/**
 * ReviewFlow's real-time engine: a room-based WebSocket hub where each room
 * corresponds to one snippet id.
 *
 * Architecture
 * ============
 *
 *          per-connection callbacks                       the Hub (shared)
 *   ┌────────────────────────────────────┐        ┌──────────────────────────────┐
 *   │  Client (1 per socket)             │        │  rooms:                      │
 *   │   • "message" → JSON               │        │    Map<snippetId,            │
 *   │       "ping"   → pong              │        │        Set<Client>>          │
 *   │       "typing" → hub.broadcast ────┼───────▶│                              │
 *   │   • "close"/"error" → unregister ──┼───────▶│  register / unregister /     │
 *   │   • ping timer  → WS ping frame    │        │  deliver — plain method      │
 *   │   • deadline timer → terminate     │◀───────┤  calls, fan-out is a         │
 *   └────────────────────────────────────┘        │  non-blocking send;          │
 *                    ▲                            │  slow clients are evicted    │
 *                    └────────────────────────────┤                              │
 *             broadcastToRoom(snippetId, msg)     └──────────────▲───────────────┘
 *                GraphQL resolvers / Gemini AI service ──────────┘
 *
 * Concurrency contract:
 *   - Node runs this on a single event-loop thread, so the rooms map needs no
 *     lock and no serializing owner. The Go original used one hub goroutine
 *     plus an RWMutex to get the same guarantee; here it is free.
 *   - Every mutation still funnels through register/unregister so presence
 *     announcements stay consistent with membership.
 *   - A client that cannot keep up (its socket buffer exceeds the watermark) is
 *     dropped rather than allowed to grow the process's memory without bound.
 */
import type { Client } from './client.ts';
import { MessageType, newMessage, type Message, type Presence } from './message.ts';

export class Hub {
  /** snippetId -> set of connected clients. */
  private readonly rooms = new Map<string, Set<Client>>();

  /**
   * Adds a client, announces a join to the room (only when this is the user's
   * first connection there), and sends the full viewer list to the newcomer.
   */
  register(client: Client): void {
    const firstForUser = this.addClient(client);

    if (firstForUser) {
      const joined = presenceFrame(MessageType.PresenceJoin, client);
      this.deliver(client.snippetId, joined, client); // everyone except the newcomer
    }

    // Seed the newcomer with who's already here.
    const list = newMessage(MessageType.PresenceList, client.snippetId, this.presence(client.snippetId));
    this.sendTo(client, JSON.stringify(list));
  }

  /**
   * Removes a client and, if that was the user's last connection in the room,
   * broadcasts a presence_leave. Used for normal disconnects and slow-client
   * eviction alike (idempotent per client).
   */
  unregister(client: Client): void {
    const { removed, lastForUser } = this.removeClient(client);
    if (removed && lastForUser) {
      this.deliver(client.snippetId, presenceFrame(MessageType.PresenceLeave, client), client);
    }
  }

  /**
   * The public entry point used by resolvers / the AI service to push a message
   * to everyone viewing a snippet.
   */
  broadcastToRoom(snippetId: string, msg: Message): void {
    msg.snippetId = snippetId;
    if (!msg.timestamp) msg.timestamp = new Date().toISOString();

    let data: string;
    try {
      data = JSON.stringify(msg);
    } catch (err) {
      console.error(`ws: marshal broadcast (snippet=${snippetId} type=${msg.type}):`, err);
      return;
    }
    this.deliver(snippetId, data);
  }

  /**
   * Relays a client-originated frame to the rest of its room. Kept on the hub
   * (rather than letting Client call deliver directly) so every fan-out path
   * goes through one place.
   */
  relay(from: Client, data: string): void {
    this.deliver(from.snippetId, data, from);
  }

  /**
   * A de-duplicated snapshot of the users currently viewing a snippet, for
   * callers outside the hub (e.g. seeding a subscription's initial state).
   */
  roomPresence(snippetId: string): Presence[] {
    return this.presence(snippetId);
  }

  /** Total live connections, used by the shutdown path and diagnostics. */
  get connectionCount(): number {
    let n = 0;
    for (const room of this.rooms.values()) n += room.size;
    return n;
  }

  /** Closes every connection. Called during graceful shutdown. */
  closeAll(): void {
    for (const room of [...this.rooms.values()]) {
      for (const client of [...room]) client.close();
    }
    this.rooms.clear();
  }

  // --- fan-out ---------------------------------------------------------------

  /**
   * Sends data to every client in a room (except `exclude`). A client whose
   * socket buffer is over the watermark is treated as a slow consumer and
   * evicted.
   *
   * Eviction recurses through unregister -> deliver(leave), but it is bounded:
   * each eviction first deletes the client from the room, so no client is ever
   * processed twice and recursion depth <= clients in the room.
   */
  private deliver(snippetId: string, data: string | null, exclude?: Client): void {
    if (data === null) return;
    for (const client of this.snapshotRoom(snippetId)) {
      if (client === exclude) continue;
      if (!client.trySend(data)) this.unregister(client);
    }
  }

  /** Pushes data to a single client, evicting it if its buffer is full. */
  private sendTo(client: Client, data: string): void {
    if (!client.trySend(data)) this.unregister(client);
  }

  // --- room bookkeeping (the only places that touch this.rooms) --------------

  /**
   * Inserts a client into its room (creating the room if needed) and reports
   * whether this is the first connection for that user there.
   */
  private addClient(client: Client): boolean {
    let room = this.rooms.get(client.snippetId);
    if (!room) {
      room = new Set<Client>();
      this.rooms.set(client.snippetId, room);
    }
    room.add(client);
    return countUser(room, client.userId) === 1;
  }

  /**
   * Deletes a client from its room and closes its socket. The membership check
   * guarantees this runs at most once per client.
   *
   * Returns whether the client was present and whether it was the user's last
   * connection in the room (so the caller knows to announce a leave).
   */
  private removeClient(client: Client): { removed: boolean; lastForUser: boolean } {
    const room = this.rooms.get(client.snippetId);
    if (!room || !room.has(client)) return { removed: false, lastForUser: false };

    room.delete(client);
    client.close();

    const lastForUser = countUser(room, client.userId) === 0;
    if (room.size === 0) this.rooms.delete(client.snippetId); // clean up empty rooms
    return { removed: true, lastForUser };
  }

  /**
   * Returns a room's clients as an array, so fan-out can iterate without being
   * disturbed by evictions mutating the underlying set.
   */
  private snapshotRoom(snippetId: string): Client[] {
    const room = this.rooms.get(snippetId);
    return room ? [...room] : [];
  }

  /**
   * A snapshot of distinct users in a room (one entry per userId, so multiple
   * tabs collapse to a single viewer).
   */
  private presence(snippetId: string): Presence[] {
    const seen = new Map<string, Presence>();
    for (const client of this.rooms.get(snippetId) ?? []) {
      if (!seen.has(client.userId)) {
        seen.set(client.userId, { userId: client.userId, username: client.username });
      }
    }
    return [...seen.values()];
  }
}

// --- small helpers -----------------------------------------------------------

/** Counts how many connections in a room belong to userId. */
function countUser(room: Set<Client>, userId: string): number {
  let n = 0;
  for (const client of room) if (client.userId === userId) n++;
  return n;
}

/**
 * Builds a serialized presence_join / presence_leave envelope for a client,
 * stamped with that client's identity. Returns null on serialization failure.
 */
function presenceFrame(type: MessageType, client: Client): string | null {
  const msg = newMessage(type, client.snippetId, {
    userId: client.userId,
    username: client.username,
  });
  msg.senderId = client.userId;
  msg.senderUsername = client.username;
  try {
    return JSON.stringify(msg);
  } catch {
    return null;
  }
}
