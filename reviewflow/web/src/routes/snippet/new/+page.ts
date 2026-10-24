import type { PageLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { initAuth, rememberNext } from '$lib/stores/auth';

export const load: PageLoad = async ({ url }) => {
	const user = await initAuth();
	if (!user) {
		rememberNext(url.pathname);
		throw redirect(307, '/');
	}
	return { user };
};
