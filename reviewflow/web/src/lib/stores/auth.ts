import { writable, type Readable } from 'svelte/store';
import { browser } from '$app/environment';
import { AUTH_ME_URL, AUTH_GITHUB_URL, AUTH_CONFIG_URL, AUTH_GUEST_URL, AUTH_LOGOUT_URL } from '$lib/config';
import type { User } from '$lib/types';

interface AuthState {
	user: User | null;
	loading: boolean;
}

export interface AuthConfig {
	github: boolean;
	guest: boolean;
	ai: boolean;
}

const TOKEN_KEY = 'rf_token';
const NEXT_KEY = 'rf_next';

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

/** Discard the memoized result so the next initAuth() re-fetches /auth/me. */
export function resetAuth(): void {
	initPromise = null;
	store.set({ user: null, loading: true });
}

export function getStoredToken(): string | null {
	if (!browser) return null;
	return localStorage.getItem(TOKEN_KEY);
}

export function storeToken(token: string): void {
	localStorage.setItem(TOKEN_KEY, token);
	resetAuth();
}

/** Remember where to go after sign-in (e.g. a shared snippet link). */
export function rememberNext(path: string): void {
	if (browser && path.startsWith('/') && !path.startsWith('//')) sessionStorage.setItem(NEXT_KEY, path);
}

/** Where to land after sign-in; consumed once. */
export function takeNext(): string {
	if (!browser) return '/dashboard';
	const next = sessionStorage.getItem(NEXT_KEY);
	sessionStorage.removeItem(NEXT_KEY);
	return next && next.startsWith('/') && !next.startsWith('//') ? next : '/dashboard';
}

async function fetchMe(): Promise<User | null> {
	try {
		const token = getStoredToken();
		const headers: Record<string, string> = {};
		if (token) headers['Authorization'] = `Bearer ${token}`;
		const res = await fetch(AUTH_ME_URL, { credentials: 'include', headers });
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

/** Which sign-in methods the server offers. */
export async function fetchAuthConfig(): Promise<AuthConfig> {
	try {
		const res = await fetch(AUTH_CONFIG_URL);
		if (res.ok) return (await res.json()) as AuthConfig;
	} catch {
		/* fall through */
	}
	return { github: true, guest: false, ai: false };
}

/** Kick off the GitHub OAuth flow by handing the browser to the backend. */
export function loginWithGitHub(): void {
	if (browser) window.location.href = AUTH_GITHUB_URL;
}

/** Sign in with just a display name (development / demo servers). */
export async function loginAsGuest(username: string): Promise<void> {
	const res = await fetch(AUTH_GUEST_URL, {
		method: 'POST',
		credentials: 'include',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ username })
	});
	const body = (await res.json().catch(() => ({}))) as { token?: string; error?: string };
	if (!res.ok || !body.token) throw new Error(body.error ?? `Sign-in failed (HTTP ${res.status})`);
	storeToken(body.token);
	await initAuth();
}

/**
 * Sign out: ask the backend to expire the HttpOnly cookie (otherwise the
 * cookie alone would keep /auth/me succeeding), then drop the local token.
 */
export async function logout(): Promise<void> {
	try {
		await fetch(AUTH_LOGOUT_URL, { method: 'POST', credentials: 'include' });
	} catch {
		/* offline: still clear local state */
	}
	store.set({ user: null, loading: false });
	initPromise = Promise.resolve(null);
	if (browser) {
		localStorage.removeItem(TOKEN_KEY);
		window.location.href = '/';
	}
}
