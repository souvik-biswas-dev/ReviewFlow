import type { LayoutLoad } from './$types';
import { initAuth } from '$lib/stores/auth';

// This app is client-rendered: it relies on an HttpOnly cookie sent to a
// cross-origin backend (credentials: 'include') and on browser-only APIs
// (WebSocket, CodeMirror). SSR would have neither, so we disable it globally.
export const ssr = false;

export const load: LayoutLoad = async () => {
	// Resolve the session once, up front; child loads reuse the memoized result.
	await initAuth();
	return {};
};
