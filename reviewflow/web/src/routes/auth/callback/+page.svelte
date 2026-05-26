<script lang="ts">
	import { browser } from '$app/environment';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { initAuth, resetAuth } from '$lib/stores/auth';

	onMount(async () => {
		const token = $page.url.searchParams.get('token');
		if (token) {
			localStorage.setItem('rf_token', token);
		}
		// +layout.ts already called initAuth() before this mount, but it ran
		// before the token was saved to localStorage so it cached a null result.
		// Reset the memo so initAuth() below re-fetches /auth/me with the token.
		resetAuth();
		await initAuth();
		goto('/dashboard', { replaceState: true });
	});
</script>

<div class="flex h-screen items-center justify-center">
	<p class="text-slate-400">Signing you in…</p>
</div>
