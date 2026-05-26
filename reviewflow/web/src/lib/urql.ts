import { createClient, cacheExchange, fetchExchange, type Client } from '@urql/svelte';
import { GRAPHQL_URL } from './config';

/**
 * Builds the urql client. `credentials: 'include'` is essential: the session
 * lives in an HttpOnly cookie on the backend's origin, and the API is on a
 * different port, so the browser must be told to send cross-origin cookies.
 */
export function createUrqlClient(): Client {
	return createClient({
		url: GRAPHQL_URL,
		exchanges: [cacheExchange, fetchExchange],
		fetchOptions: {
			credentials: 'include'
		}
	});
}
