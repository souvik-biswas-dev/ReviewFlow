<script lang="ts">
	import { browser } from '$app/environment';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { initAuth } from '$lib/stores/auth';

	onMount(async () => {
		const token = $page.url.searchParams.get('token');
		if (token) {
			localStorage.setItem('rf_token', token);
		}
		await initAuth();
		goto('/dashboard', { replaceState: true });
	});
</script>

<div class="flex h-screen items-center justify-center">
	<p class="text-slate-400">Signing you in…</p>
</div>
