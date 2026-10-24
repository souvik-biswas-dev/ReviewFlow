import { env } from '$env/dynamic/public';

/**
 * Base URL of the Go (Gin) backend (queries, auth, websockets all hang off this).
 *
 * Resolution order:
 *   1. PUBLIC_API_URL env var (leave unset in dev: the Vite proxy forwards
 *      /auth /graphql /ws /notifications to the backend, so everything is
 *      same-origin and there are no CORS or cookie surprises).
 *   2. window.location.origin — the prod case, where SvelteKit and the Go
 *      backend are both served from the same Caddy host, so a relative origin
 *      works for HTTP and WebSocket alike.
 *   3. A safe localhost default for any remaining edge cases.
 */
function resolveApiUrl(): string {
	const fromEnv = env.PUBLIC_API_URL;
	if (fromEnv) return fromEnv;
	if (typeof window !== 'undefined') return window.location.origin;
	return 'http://localhost:8080';
}

export const API_URL: string = resolveApiUrl();

/** WebSocket origin, derived from API_URL (http -> ws, https -> wss). */
export const WS_URL: string = API_URL.replace(/^http/, 'ws');

export const GRAPHQL_URL = `${API_URL}/graphql`;
export const AUTH_GITHUB_URL = `${API_URL}/auth/github`;
export const AUTH_ME_URL = `${API_URL}/auth/me`;
export const AUTH_CONFIG_URL = `${API_URL}/auth/config`;
export const AUTH_GUEST_URL = `${API_URL}/auth/guest`;
export const AUTH_LOGOUT_URL = `${API_URL}/auth/logout`;
export const NOTIFICATIONS_URL = `${API_URL}/notifications`;

/** WebSocket endpoint for a snippet room. */
export const wsUrlFor = (snippetId: string): string => `${WS_URL}/ws/${snippetId}`;
