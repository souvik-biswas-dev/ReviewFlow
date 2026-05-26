<script lang="ts">
	import { getContextClient } from '@urql/svelte';
	import { Plus, LogOut, AlertTriangle, FileCode2 } from 'lucide-svelte';
	import type { PageData } from './$types';
	import { GET_SNIPPETS } from '$lib/graphql/snippets';
	import { LANGUAGES, languageLabel } from '$lib/codemirror/languages';
	import { logout } from '$lib/stores/auth';
	import SnippetCard from '$lib/components/SnippetCard.svelte';
	import Avatar from '$lib/components/ui/Avatar.svelte';
	import Select from '$lib/components/ui/Select.svelte';
	import NotificationBell from '$lib/components/NotificationBell.svelte';
	import type { SnippetCardData } from '$lib/types';

	let { data }: { data: PageData } = $props();
	const client = getContextClient();

	let langFilter = $state('');
	let snippets = $state<SnippetCardData[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	const langOptions = [
		{ value: '', label: 'All languages' },
		...LANGUAGES.map((l) => ({ value: l, label: languageLabel(l) }))
	];

	// Re-fetch whenever the language filter changes.
	$effect(() => {
		const lang = langFilter;
		loading = true;
		error = null;
		client
			.query(GET_SNIPPETS, { language: lang || undefined, limit: 60 })
			.toPromise()
			.then((res) => {
				if (res.error) {
					error = res.error.message;
					snippets = [];
				} else {
					snippets = (res.data?.snippets ?? []) as SnippetCardData[];
				}
				loading = false;
			});
	});
</script>

<svelte:head>
	<title>Dashboard · ReviewFlow</title>
</svelte:head>

<div class="mx-auto flex min-h-screen max-w-7xl flex-col px-6">
	<!-- Top bar -->
	<header class="flex items-center justify-between py-6">
		<a href="/dashboard" class="flex items-center gap-2 font-display text-lg font-bold">
			<span class="text-aurora">✦</span> ReviewFlow
		</a>
		<div class="flex items-center gap-1">
			<NotificationBell />
			<button
				onclick={logout}
				class="inline-flex items-center gap-2 rounded-lg px-3 py-2 text-sm text-slate-400 transition hover:bg-white/[0.05] hover:text-slate-200"
			>
				<LogOut class="h-4 w-4" /> Sign out
			</button>
		</div>
	</header>

	<div class="flex flex-1 flex-col gap-6 pb-16 lg:flex-row">
		<!-- Sidebar -->
		<aside class="lg:w-72 lg:shrink-0">
			<div class="glass sticky top-6 space-y-5 rounded-2xl p-5">
				<div class="flex items-center gap-3">
					<Avatar src={data.user.avatarUrl} name={data.user.githubUsername} size={48} ring />
					<div class="min-w-0">
						<p class="truncate font-display font-semibold text-slate-100">
							{data.user.githubUsername}
						</p>
						<p class="text-xs text-slate-500">Signed in</p>
					</div>
				</div>

				<a href="/snippet/new" class="btn-aurora w-full">
					<Plus class="h-4 w-4" /> New Snippet
				</a>

				<div>
					<label for="lang-filter" class="mb-1.5 block text-xs font-medium text-slate-400">
						Filter by language
					</label>
					<Select bind:value={langFilter} options={langOptions} placeholder="All languages" />
				</div>
			</div>
		</aside>

		<!-- Main -->
		<main class="flex-1">
			<div class="mb-5 flex items-end justify-between">
				<h1 class="font-display text-2xl font-bold text-slate-100">Snippets</h1>
				{#if !loading && !error}
					<span class="text-sm text-slate-500">{snippets.length} total</span>
				{/if}
			</div>

			{#if loading}
				<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
					{#each Array(6) as _, i (i)}
						<div class="skeleton h-44 rounded-2xl"></div>
					{/each}
				</div>
			{:else if error}
				<div class="glass flex items-center gap-3 rounded-2xl p-6 text-sm text-red-300">
					<AlertTriangle class="h-5 w-5 shrink-0" />
					<div>
						<p class="font-medium">Couldn't load snippets</p>
						<p class="text-red-300/70">{error}</p>
					</div>
				</div>
			{:else if snippets.length === 0}
				<div class="glass flex flex-col items-center gap-3 rounded-2xl p-12 text-center">
					<FileCode2 class="h-8 w-8 text-slate-600" />
					<p class="font-display text-lg font-semibold text-slate-200">No snippets yet</p>
					<p class="max-w-sm text-sm text-slate-500">
						Create your first snippet and let the AI — and your team — take a look.
					</p>
					<a href="/snippet/new" class="btn-aurora mt-2"><Plus class="h-4 w-4" /> New Snippet</a>
				</div>
			{:else}
				<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
					{#each snippets as snippet (snippet.id)}
						<SnippetCard {snippet} />
					{/each}
				</div>
			{/if}
		</main>
	</div>
</div>
