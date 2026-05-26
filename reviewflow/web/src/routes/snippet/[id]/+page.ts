import type { PageLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { initAuth } from '$lib/stores/auth';

export const load: PageLoad = async ({ params }) => {
	const user = await initAuth();
	if (!user) throw redirect(307, '/');
	return { id: params.id, user };
};
