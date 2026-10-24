<script lang="ts">
	import { Github, Radio, Sparkles, ShieldCheck, ArrowRight, ExternalLink } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { loginWithGitHub, loginAsGuest, fetchAuthConfig, takeNext, type AuthConfig } from '$lib/stores/auth';

	let config = $state<AuthConfig | null>(null);
	let guestName = $state('');
	let signingIn = $state(false);
	let signInError = $state<string | null>(null);

	onMount(async () => {
		config = await fetchAuthConfig();
	});

	async function guestSignIn(e: Event): Promise<void> {
		e.preventDefault();
		if (signingIn) return;
		signingIn = true;
		signInError = null;
		try {
			await loginAsGuest(guestName.trim());
			await goto(takeNext());
		} catch (err) {
			signInError = err instanceof Error ? err.message : 'Sign-in failed.';
		} finally {
			signingIn = false;
		}
	}

	const stack = [
		{ name: 'SvelteKit', slug: 'svelte' },
		{ name: 'Go', slug: 'go' },
		{ name: 'GraphQL', slug: 'graphql' },
		{ name: 'MongoDB', slug: 'mongodb' },
		{ name: 'Cloudflare', slug: 'cloudflare' },
		{ name: 'Render', slug: 'render' }
	];

	const features = [
		{
			icon: Radio,
			title: 'Real-time rooms',
			body: 'Reviews, presence, and typing indicators stream live over WebSockets — see who is reading your code as they read it.'
		},
		{
			icon: Sparkles,
			title: 'AI that reads first',
			body: 'Gemini analyzes every snippet the moment you post it: quality score, complexity, refactor hints, and security flags.'
		},
		{
			icon: ShieldCheck,
			title: 'Share a link, review together',
			body: 'Sign in, send the snippet link to a teammate, and review side by side. Notifications tell authors the moment someone weighs in.'
		}
	];
</script>

<div class="mx-auto flex min-h-screen max-w-6xl flex-col px-6">
	<!-- Nav -->
	<header class="flex items-center justify-between py-6">
		<div class="flex items-center gap-2 font-display text-lg font-bold">
			<span class="text-aurora">✦</span> ReviewFlow
		</div>
		<a href="#signin" class="btn-ghost">Sign in</a>
	</header>

	<!-- Hero -->
	<section class="flex flex-1 flex-col items-center justify-center py-16 text-center">
		<div class="animate-fade-up">
			<Badge tone="violet"><Sparkles class="h-3 w-3" /> AI-assisted code review</Badge>
		</div>

		<h1
			class="mt-6 max-w-4xl animate-fade-up font-display text-5xl font-bold leading-[1.05] tracking-tight sm:text-7xl [animation-delay:60ms]"
		>
			Review code <span class="text-aurora">together</span>, in real time — with an
			<span class="text-aurora">AI</span> in the room.
		</h1>

		<p
			class="mt-6 max-w-2xl animate-fade-up text-lg leading-relaxed text-slate-400 [animation-delay:120ms]"
		>
			ReviewFlow pairs live, multiplayer code review with instant Gemini analysis. Drop in a
			snippet, share the link, and watch feedback arrive as it happens.
		</p>

		<div id="signin" class="mt-10 w-full max-w-sm animate-fade-up space-y-3 [animation-delay:180ms]">
			{#if config === null}
				<div class="skeleton mx-auto h-12 w-full rounded-xl"></div>
			{:else}
				{#if config.github}
					<Button onclick={loginWithGitHub} class="w-full">
						<Github class="h-5 w-5" /> Sign in with GitHub
						<ArrowRight class="h-4 w-4" />
					</Button>
				{/if}
				{#if config.github && config.guest}
					<p class="text-xs uppercase tracking-wide text-slate-500">or</p>
				{/if}
				{#if config.guest}
					<form onsubmit={guestSignIn} class="glass flex gap-2 rounded-xl p-2">
						<input
							bind:value={guestName}
							placeholder="Pick a display name"
							maxlength="32"
							autocomplete="nickname"
							aria-label="Display name"
							class="min-w-0 flex-1 rounded-lg bg-transparent px-3 py-2 text-slate-200 placeholder:text-slate-500 focus:outline-none"
						/>
						<Button type="submit" disabled={signingIn || guestName.trim().length < 2}>
							{signingIn ? 'Joining…' : 'Continue as guest'}
						</Button>
					</form>
					<p class="text-xs text-slate-500">
						No account needed. Open a second browser window with another name to try live
						reviews.
					</p>
				{/if}
				{#if !config.github && !config.guest}
					<p class="glass rounded-xl p-4 text-sm text-amber-300">
						No sign-in method is enabled on this server. Set ALLOW_GUEST_LOGIN=true or configure
						GitHub OAuth in .env.
					</p>
				{/if}
				{#if signInError}
					<p class="text-sm text-red-400">{signInError}</p>
				{/if}
			{/if}
		</div>
	</section>

	<!-- Feature grid -->
	<section class="grid grid-cols-1 gap-4 md:grid-cols-3">
		{#each features as f, i (f.title)}
			<div
				class="glass glass-hover animate-fade-up rounded-2xl p-6"
				style={`animation-delay:${240 + i * 80}ms`}
			>
				<div
					class="flex h-11 w-11 items-center justify-center rounded-xl bg-aurora-grad/20 text-aurora-violet"
				>
					<f.icon class="h-5 w-5" />
				</div>
				<h3 class="mt-4 font-display text-lg font-semibold text-slate-100">{f.title}</h3>
				<p class="mt-2 text-sm leading-relaxed text-slate-400">{f.body}</p>
			</div>
		{/each}
	</section>

	<!-- Footer -->
	<footer class="mt-16 border-t border-white/5 py-10 text-center">
		<p class="text-sm text-slate-400">
			Designed, architected &amp; engineered by
			<a
				href="https://souvikbiswas-portfolio.pages.dev"
				target="_blank"
				rel="noopener noreferrer"
				class="font-semibold text-slate-200 transition hover:text-aurora-violet"
			>
				Souvik Biswas
			</a>
		</p>

		<div class="mt-4 flex flex-wrap items-center justify-center gap-5">
			{#each stack as s (s.slug)}
				<div class="flex flex-col items-center gap-1.5 opacity-50 transition hover:opacity-100" title={s.name}>
					<img
						src={`https://cdn.simpleicons.org/${s.slug}/ffffff`}
						alt={s.name}
						class="h-5 w-5"
					/>
					<span class="text-[10px] text-slate-500">{s.name}</span>
				</div>
			{/each}
		</div>

		<div class="mt-5 flex items-center justify-center gap-6 text-sm text-slate-500">
			<a
				href="https://souvikbiswas-portfolio.pages.dev"
				target="_blank"
				rel="noopener noreferrer"
				class="flex items-center gap-1.5 transition hover:text-slate-300"
			>
				<ExternalLink class="h-3.5 w-3.5" /> Portfolio
			</a>
			<a
				href="https://github.com/souvik-biswas-dev/"
				target="_blank"
				rel="noopener noreferrer"
				class="flex items-center gap-1.5 transition hover:text-slate-300"
			>
				<Github class="h-3.5 w-3.5" /> GitHub
			</a>
		</div>
	</footer>
</div>
