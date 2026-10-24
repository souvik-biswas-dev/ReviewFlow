import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// Where the Go backend lives. In docker compose this is http://server:8080.
const backend = process.env.BACKEND_URL ?? 'http://localhost:8080';

// NOTE: the SPA's own routes must never start with a proxied prefix. The
// post-login page is /login/callback (not /auth/callback) for exactly that
// reason: /auth/* belongs to the backend.
export default defineConfig({
	plugins: [sveltekit()],
	server: {
		port: 5173,
		proxy: {
			'/auth': backend,
			'/graphql': backend,
			'/notifications': backend,
			'/health': backend,
			'/ws': { target: backend.replace(/^http/, 'ws'), ws: true }
		}
	}
});
