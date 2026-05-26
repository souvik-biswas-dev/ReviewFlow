// Domain types mirroring the Go backend's GraphQL schema + WebSocket envelope.

export interface User {
	id: string;
	githubUsername: string;
	avatarUrl: string;
	createdAt: string;
}

export interface Review {
	id: string;
	snippetId: string;
	author: User;
	body: string;
	lineNumber: number | null;
	parentReviewId: string | null;
	createdAt: string;
}

export interface AIReview {
	id: string;
	snippetId: string;
	suggestions: string[];
	complexity: string;
	refactorHints: string[];
	securityFlags: string[];
	qualityScore: number;
	language: string;
	generatedAt: string;
}

export interface Snippet {
	id: string;
	title: string;
	language: string;
	code: string;
	previousVersion: string | null;
	author: User;
	reviews: Review[];
	aiReview: AIReview | null;
	createdAt: string;
	updatedAt: string;
}

/** Row returned by GET /notifications (enriched with the snippet title). */
export interface NotificationItem {
	id: string;
	snippetId: string;
	snippetTitle: string;
	reviewId: string;
	read: boolean;
	createdAt: string;
}

export interface NotificationsResponse {
	notifications: NotificationItem[];
	unreadCount: number;
	page: number;
}

/** Lightweight shape used by the dashboard grid (subset of Snippet). */
export interface SnippetCardData {
	id: string;
	title: string;
	language: string;
	createdAt: string;
	author: Pick<User, 'githubUsername' | 'avatarUrl'>;
	reviews: { id: string }[];
	aiReview: { id: string } | null;
}

// ---- WebSocket protocol -----------------------------------------------------

export type WsMessageType =
	| 'review_added'
	| 'presence_join'
	| 'presence_leave'
	| 'presence_list'
	| 'ai_review_ready'
	| 'typing'
	| 'ping'
	| 'pong';

export interface Presence {
	userId: string;
	username: string;
}

/** The envelope every WS frame is wrapped in (matches internal/ws.Message). */
export interface WsMessage<T = unknown> {
	type: WsMessageType;
	snippetId: string;
	payload: T;
	senderId?: string;
	senderUsername?: string;
	timestamp: string;
}

/** Maps each message type to its decoded payload shape. */
export interface WsPayloadMap {
	review_added: Review;
	presence_join: Presence;
	presence_leave: Presence;
	presence_list: Presence[];
	ai_review_ready: AIReview;
	typing: { line?: number } | null;
	ping: null;
	pong: null;
}
