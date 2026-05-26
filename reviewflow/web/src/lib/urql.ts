import { createClient, cacheExchange, fetchExchange, type Client } from '@urql/svelte';
import { GRAPHQL_URL } from './config';
import { getStoredToken } from './stores/auth';

export function createUrqlClient(): Client {
	return createClient({
		url: GRAPHQL_URL,
		exchanges: [cacheExchange, fetchExchange],
		fetchOptions: () => {
			const token = getStoredToken();
			return {
				credentials: 'include',
				headers: token ? { Authorization: `Bearer ${token}` } : {}
			};
		}
	});
}
