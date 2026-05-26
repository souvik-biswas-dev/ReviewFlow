import { writable, type Readable } from 'svelte/store';
import { browser } from '$app/environment';
import { wsUrlFor } from '$lib/config';
import type { WsMessage, WsMessageType, WsPayloadMap } from '$lib/types';

export type WsStatus = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed';

type Handler<K extends WsMessageType> = (
	payload: WsPayloadMap[K],
	msg: WsMessage<WsPayloadMap[K]>
) => void;

export interface WsStore {
	/** Reactive connection status. */
	status: Readable<WsStatus>;
	connect(snippetId: string): void;
	disconnect(): void;
	/** Send a typed frame. snippetId/timestamp are filled in automatically. */
	send<K extends WsMessageType>(type: K, payload?: WsPayloadMap[K]): void;
	/** Subscribe to a message type. Returns an unsubscribe function. */
	on<K extends WsMessageType>(type: K, handler: Handler<K>): () => void;
}

// Reconnect schedule: exponential backoff up to ~2 minutes.
// Render's free tier can take 50 s+ to cold-start, so we keep retrying well
// past that window before giving up.
const BACKOFF_MS = [1000, 2000, 4000, 8000, 16000, 32000, 60000];
const HEARTBEAT_MS = 25_000;

/**
 * Creates an isolated WebSocket manager. One instance backs one snippet page;
 * it owns a single socket, reconnects on unexpected drops, and fans incoming
 * frames out to typed listeners.
 */
export function createWsStore(): WsStore {
	const status = writable<WsStatus>('idle');

	let socket: WebSocket | null = null;
	let snippetId: string | null = null;
	let attempt = 0;
	let manualClose = false;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let heartbeat: ReturnType<typeof setInterval> | null = null;

	// Per-type listener registry. `Set` so the same handler isn't double-fired.
	const listeners = new Map<WsMessageType, Set<Handler<WsMessageType>>>();

	function emit(msg: WsMessage): void {
		const set = listeners.get(msg.type);
		if (!set) return;
		for (const handler of set) {
			try {
				(handler as (p: unknown, m: WsMessage) => void)(msg.payload, msg);
			} catch (err) {
				console.error('[ws] listener error', err);
			}
		}
	}

	// Local sender used both by the heartbeat and the public `send` below.
	function send<K extends WsMessageType>(type: K, payload?: WsPayloadMap[K]): void {
		if (!socket || socket.readyState !== WebSocket.OPEN || !snippetId) return;
		const frame: WsMessage = {
			type,
			snippetId,
			payload: payload ?? null,
			timestamp: new Date().toISOString()
		};
		socket.send(JSON.stringify(frame));
	}

	function clearTimers(): void {
		if (reconnectTimer) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
		if (heartbeat) {
			clearInterval(heartbeat);
			heartbeat = null;
		}
	}

	function open(): void {
		if (!browser || !snippetId) return;

		status.set(attempt === 0 ? 'connecting' : 'reconnecting');
		const token = localStorage.getItem('rf_token');
		const url = wsUrlFor(snippetId) + (token ? `?token=${encodeURIComponent(token)}` : '');
		const ws = new WebSocket(url);
		socket = ws;

		ws.onopen = () => {
			attempt = 0;
			status.set('open');
			// App-level heartbeat: browsers can't send WS ping frames from JS,
			// and the backend treats any inbound frame as liveness.
			heartbeat = setInterval(() => send('ping'), HEARTBEAT_MS);
		};

		ws.onmessage = (event: MessageEvent<string>) => {
			let msg: WsMessage;
			try {
				msg = JSON.parse(event.data) as WsMessage;
			} catch {
				return; // ignore non-JSON frames
			}
			emit(msg);
		};

		ws.onclose = () => {
			socket = null;
			clearTimers();
			if (manualClose) {
				status.set('closed');
				return;
			}
			scheduleReconnect();
		};

		ws.onerror = () => {
			// onclose fires right after; let it own the reconnect decision.
			ws.close();
		};
	}

	function scheduleReconnect(): void {
		if (attempt >= BACKOFF_MS.length) {
			status.set('closed');
			console.warn('[ws] giving up after', BACKOFF_MS.length, 'attempts');
			return;
		}
		const delay = BACKOFF_MS[attempt];
		attempt += 1;
		status.set('reconnecting');
		reconnectTimer = setTimeout(open, delay);
	}

	return {
		status: { subscribe: status.subscribe },

		connect(id: string) {
			if (!browser) return;
			// Reconnecting to the same room that's already open is a no-op.
			if (snippetId === id && socket && socket.readyState === WebSocket.OPEN) return;
			this.disconnect();
			snippetId = id;
			manualClose = false;
			attempt = 0;
			open();
		},

		disconnect() {
			manualClose = true;
			clearTimers();
			if (socket) {
				socket.onclose = null; // prevent reconnect from our own close
				socket.close();
				socket = null;
			}
			status.set('closed');
		},

		send,

		on(type, handler) {
			let set = listeners.get(type);
			if (!set) {
				set = new Set();
				listeners.set(type, set);
			}
			set.add(handler as Handler<WsMessageType>);
			return () => set?.delete(handler as Handler<WsMessageType>);
		}
	};
}
