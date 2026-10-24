import type { PageLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { initAuth, rememberNext } from '$lib/stores/auth';

export const load: PageLoad = async ({ params, url }) => {
	const user = await initAuth();
	if (!user) {
		// A shared link: sign in first, then come straight back here.
		rememberNext(url.pathname);
		throw redirect(307, '/');
	}
	return { id: params.id, user };
};
