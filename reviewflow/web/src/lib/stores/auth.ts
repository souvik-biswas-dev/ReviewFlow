import { writable, type Readable } from 'svelte/store';
import { browser } from '$app/environment';
import { AUTH_ME_URL, AUTH_GITHUB_URL } from '$lib/config';
import type { User } from '$lib/types';

interface AuthState {
	user: User | null;
	loading: boolean;
}

const store = writable<AuthState>({ user: null, loading: true });

/** Public, read-only view of the auth state. */
export const auth: Readable<AuthState> = { subscribe: store.subscribe };

// initAuth is memoized so /+layout.ts and every protected /+page.ts can all
// `await initAuth()` while only one /auth/me request is ever made.
let initPromise: Promise<User | null> | null = null;

export function initAuth(): Promise<User | null> {
	if (!browser) return Promise.resolve(null);
	if (!initPromise) initPromise = fetchMe();
	return initPromise;
}

async function fetchMe(): Promise<User | null> {
	try {
		const res = await fetch(AUTH_ME_URL, { credentials: 'include' });
		if (!res.ok) {
			store.set({ user: null, loading: false });
			return null;
		}
		const data = (await res.json()) as User;
		store.set({ user: data, loading: false });
		return data;
	} catch {
		store.set({ user: null, loading: false });
		return null;
	}
}

/** Kick off the GitHub OAuth flow by handing the browser to the backend. */
export function loginWithGitHub(): void {
	if (browser) window.location.href = AUTH_GITHUB_URL;
}

/**
 * Clear local auth state. (The backend exposes no logout endpoint yet, so we
 * just drop the in-memory user and return home; the cookie expires after 7d.)
 */
export function logout(): void {
	store.set({ user: null, loading: false });
	initPromise = Promise.resolve(null);
	if (browser) window.location.href = '/';
}
