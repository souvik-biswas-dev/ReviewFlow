<script lang="ts">
	import { Github, Radio, Sparkles, ShieldCheck, ArrowRight, ExternalLink } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import { loginWithGitHub } from '$lib/stores/auth';

	const stack = [
		'SvelteKit · Svelte 5',
		'Go · Gin',
		'GraphQL · gqlgen',
		'MongoDB',
		'WebSockets',
		'Gemini AI',
		'Cloudflare Pages',
		'Render'
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
			title: 'GitHub-native',
			body: 'Sign in with GitHub, share a link, and review together. No setup, no installs, no friction.'
		}
	];
</script>

<div class="mx-auto flex min-h-screen max-w-6xl flex-col px-6">
	<!-- Nav -->
	<header class="flex items-center justify-between py-6">
		<div class="flex items-center gap-2 font-display text-lg font-bold">
			<span class="text-aurora">✦</span> ReviewFlow
		</div>
		<Button variant="ghost" onclick={loginWithGitHub}>
			<Github class="h-4 w-4" /> Sign in
		</Button>
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

		<div class="mt-10 flex animate-fade-up items-center gap-3 [animation-delay:180ms]">
			<Button onclick={loginWithGitHub}>
				<Github class="h-5 w-5" /> Sign in with GitHub
				<ArrowRight class="h-4 w-4" />
			</Button>
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

		<div class="mt-4 flex flex-wrap items-center justify-center gap-2">
			{#each stack as s (s)}
				<span
					class="rounded-full border border-white/10 bg-white/[0.03] px-3 py-1 text-xs text-slate-500"
				>
					{s}
				</span>
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
				href="https://github.com/devsouvikbiswas"
				target="_blank"
				rel="noopener noreferrer"
				class="flex items-center gap-1.5 transition hover:text-slate-300"
			>
				<Github class="h-3.5 w-3.5" /> GitHub
			</a>
		</div>
	</footer>
</div>
