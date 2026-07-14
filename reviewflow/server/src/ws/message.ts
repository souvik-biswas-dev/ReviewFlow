/** The kinds of envelopes that flow over the socket. */
export const MessageType = {
  ReviewAdded: 'review_added', // a new review was posted
  PresenceJoin: 'presence_join', // a user joined the room
  PresenceLeave: 'presence_leave', // a user left the room
  PresenceList: 'presence_list', // full current viewer list
  AIReviewReady: 'ai_review_ready', // Gemini analysis complete
  Typing: 'typing', // a user is typing a review
  Ping: 'ping', // app-level keepalive (client -> server)
  Pong: 'pong', // app-level keepalive (server -> client)
} as const;

export type MessageType = (typeof MessageType)[keyof typeof MessageType];

/**
 * The JSON envelope exchanged with every client.
 *
 * payload is deliberately untyped: the envelope is type-agnostic so the hub can
 * shuffle messages between rooms without understanding each payload's concrete
 * shape. Producers build the payload (a Presence, a review object, ...) and the
 * receiving client decodes it based on `type`.
 */
export interface Message {
  type: MessageType;
  snippetId: string;
  payload?: unknown;
  senderId?: string;
  senderUsername?: string;
  timestamp: string; // RFC-3339 / ISO-8601, UTC
}

/**
 * Identifies a viewer. It is the payload for presence_join / presence_leave (a
 * single Presence) and presence_list (a Presence[]).
 */
export interface Presence {
  userId: string;
  username: string;
}

/** Builds an envelope, stamping the current time. */
export function newMessage(type: MessageType, snippetId: string, payload?: unknown): Message {
  const msg: Message = { type, snippetId, timestamp: new Date().toISOString() };
  if (payload !== undefined && payload !== null) msg.payload = payload;
  return msg;
}
