<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { initAuth, storeToken, takeNext } from '$lib/stores/auth';

	let error = $state<string | null>(null);

	// The backend returns here with #token=... (or #error=...). A URL fragment is
	// never sent to any server, so the token does not end up in access logs.
	onMount(async () => {
		const hash = new URLSearchParams(window.location.hash.replace(/^#/, ''));
		const failure = hash.get('error');
		const token = hash.get('token');
		history.replaceState(null, '', window.location.pathname); // scrub it from the address bar

		if (failure || !token) {
			error = failure ?? 'No sign-in token was returned.';
			return;
		}
		storeToken(token);
		const user = await initAuth();
		if (!user) {
			error = 'Signed in, but the session could not be verified. Please try again.';
			return;
		}
		await goto(takeNext(), { replaceState: true });
	});
</script>

<div class="flex h-screen items-center justify-center px-6">
	{#if error}
		<div class="glass max-w-md rounded-2xl p-6 text-center">
			<p class="font-display text-lg font-semibold text-slate-100">Sign-in failed</p>
			<p class="mt-2 text-sm text-red-300">{error}</p>
			<a href="/" class="btn-aurora mt-5 inline-flex">Back to start</a>
		</div>
	{:else}
		<p class="text-slate-400">Signing you in…</p>
	{/if}
</div>
