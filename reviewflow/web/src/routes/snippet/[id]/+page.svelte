<script lang="ts">
	import { onMount } from 'svelte';
	import { getContextClient } from '@urql/svelte';
	import { ArrowLeft, AlertTriangle, Code as CodeIcon, GitCompareArrows } from 'lucide-svelte';
	import type { PageData } from './$types';

	import { GET_SNIPPET, ADD_REVIEW } from '$lib/graphql/snippets';
	import { createWsStore } from '$lib/stores/ws';
	import { languageLabel } from '$lib/codemirror/languages';
	import { cn } from '$lib/utils/cn';
	import type { Snippet, Review, AIReview, Presence, WsMessage } from '$lib/types';

	import CodeEditor from '$lib/components/CodeEditor.svelte';
	import DiffView from '$lib/components/DiffView.svelte';
	import Tabs from '$lib/components/ui/Tabs.svelte';
	import Badge from '$lib/components/ui/Badge.svelte';
	import Avatar from '$lib/components/ui/Avatar.svelte';
	import PresenceBar from '$lib/components/PresenceBar.svelte';
	import ThreadedReviewList from '$lib/components/ThreadedReviewList.svelte';
	import ReviewComposer from '$lib/components/ReviewComposer.svelte';
	import AIReviewPanel from '$lib/components/AIReviewPanel.svelte';

	let { data }: { data: PageData } = $props();
	const client = getContextClient();
	const ws = createWsStore();
	const wsStatus = ws.status;

	// --- page state ---
	let snippet = $state<Snippet | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let reviews = $state<Review[]>([]);
	let aiReview = $state<AIReview | null>(null);
	let aiPending = $state(false);
	let aiPendingTimer: ReturnType<typeof setTimeout> | null = null;

	let viewers = $state<Presence[]>([]);
	let typingNames = $state<string[]>([]);
	const typingTimers = new Map<string, ReturnType<typeof setTimeout>>();

	let activeTab = $state('reviews');
	let activeLine = $state<number | null>(null);
	let replyTo = $state<Review | null>(null);
	let composerError = $state<string | null>(null);

	// Code vs Diff toggle (only meaningful when a previous version exists).
	let codeView = $state<'code' | 'diff'>('code');
	const hasDiff = $derived(!!snippet?.previousVersion);

	const tabs = $derived([
		{ id: 'reviews', label: 'Reviews', count: reviews.length },
		{ id: 'ai', label: 'AI Review' }
	]);

	// --- data ---
	async function loadSnippet(): Promise<void> {
		loading = true;
		error = null;
		const res = await client.query(GET_SNIPPET, { id: data.id }).toPromise();
		if (res.error) {
			error = res.error.message;
		} else if (!res.data?.snippet) {
			error = 'Snippet not found.';
		} else {
			const s = res.data.snippet as Snippet;
			snippet = s;
			reviews = s.reviews ?? [];
			aiReview = s.aiReview;
			aiPending = !s.aiReview;
			// If the AI review hasn't arrived yet, give it 60 s before
			// switching to the "not available" state — prevents an infinite
			// skeleton when the backend AI job failed silently.
			if (aiPending) {
				if (aiPendingTimer) clearTimeout(aiPendingTimer);
				aiPendingTimer = setTimeout(() => {
					if (aiPending) aiPending = false;
				}, 60_000);
			}
		}
		loading = false;
	}

	function upsertReview(r: Review): void {
		if (!reviews.some((x) => x.id === r.id)) reviews = [...reviews, r];
	}

	async function submitReview(body: string, lineNumber: number | null): Promise<void> {
		composerError = null;
		const res = await client
			.mutation(ADD_REVIEW, {
				snippetId: data.id,
				input: {
					body,
					lineNumber,
					parentReviewId: replyTo?.id ?? null
				}
			})
			.toPromise();
		if (res.error || !res.data?.addReview) {
			composerError = res.error?.message ?? 'Could not post review.';
			return;
		}
		// Optimistic local add; the WS review_added is de-duplicated by id.
		upsertReview(res.data.addReview as Review);
	}

	function notifyTyping(): void {
		ws.send('typing', activeLine != null ? { line: activeLine } : null);
	}

	function handleTyping(msg: WsMessage): void {
		const name = msg.senderUsername;
		if (!name || msg.senderId === data.user.id) return; // ignore our own
		if (!typingNames.includes(name)) typingNames = [...typingNames, name];
		const prev = typingTimers.get(name);
		if (prev) clearTimeout(prev);
		typingTimers.set(
			name,
			setTimeout(() => {
				typingNames = typingNames.filter((n) => n !== name);
				typingTimers.delete(name);
			}, 3000)
		);
	}

	function handleLineClick(line: number): void {
		activeLine = line;
		activeTab = 'reviews';
	}

	function handleReply(review: Review): void {
		replyTo = review;
		activeLine = null; // a reply isn't anchored to a code line
		activeTab = 'reviews';
	}

	// --- lifecycle ---
	onMount(() => {
		void loadSnippet();
		ws.connect(data.id);

		const offs = [
			ws.on('presence_list', (p) => (viewers = p)),
			ws.on('presence_join', (p) => {
				if (!viewers.some((v) => v.userId === p.userId)) viewers = [...viewers, p];
			}),
			ws.on('presence_leave', (p) => (viewers = viewers.filter((v) => v.userId !== p.userId))),
			ws.on('review_added', (r) => upsertReview(r)),
			ws.on('ai_review_ready', (r) => {
				if (aiPendingTimer) { clearTimeout(aiPendingTimer); aiPendingTimer = null; }
				aiReview = r;
				aiPending = false;
			}),
			ws.on('typing', (_, msg) => handleTyping(msg))
		];

		return () => {
			offs.forEach((off) => off());
			ws.disconnect();
			if (aiPendingTimer) clearTimeout(aiPendingTimer);
			for (const t of typingTimers.values()) clearTimeout(t);
		};
	});
</script>

<svelte:head>
	<title>{snippet ? `${snippet.title} · ReviewFlow` : 'ReviewFlow'}</title>
</svelte:head>

<div class="flex h-screen flex-col">
	<!-- Presence bar / header -->
	<header class="flex items-center justify-between gap-4 border-b border-white/5 px-5 py-3.5">
		<div class="flex min-w-0 items-center gap-3">
			<a
				href="/dashboard"
				class="inline-flex h-9 w-9 items-center justify-center rounded-lg text-slate-400 transition hover:bg-white/[0.05] hover:text-slate-200"
				aria-label="Back to dashboard"
			>
				<ArrowLeft class="h-4 w-4" />
			</a>
			{#if snippet}
				<div class="min-w-0">
					<div class="flex items-center gap-2">
						<h1 class="truncate font-display text-base font-semibold text-slate-100">
							{snippet.title}
						</h1>
						<Badge tone="cyan">{languageLabel(snippet.language)}</Badge>
					</div>
					<div class="mt-0.5 flex items-center gap-1.5 text-xs text-slate-500">
						<Avatar
							src={snippet.author.avatarUrl}
							name={snippet.author.githubUsername}
							size={16}
						/>
						{snippet.author.githubUsername}
					</div>
				</div>
			{/if}
		</div>

		<div class="flex items-center gap-4">
			{#if $wsStatus === 'connecting' || $wsStatus === 'reconnecting'}
				<span class="text-xs text-amber-400">● connecting…</span>
			{:else if $wsStatus === 'closed'}
				<span class="text-xs text-slate-500">● offline</span>
			{/if}
			<PresenceBar {viewers} />
		</div>
	</header>

	<!-- Body -->
	{#if loading}
		<div class="grid flex-1 grid-cols-1 gap-4 p-4 lg:grid-cols-5">
			<div class="skeleton rounded-2xl lg:col-span-3"></div>
			<div class="skeleton rounded-2xl lg:col-span-2"></div>
		</div>
	{:else if error}
		<div class="flex flex-1 items-center justify-center p-6">
			<div class="glass flex items-center gap-3 rounded-2xl p-6 text-sm text-red-300">
				<AlertTriangle class="h-5 w-5 shrink-0" />
				<div>
					<p class="font-medium">Couldn't load this snippet</p>
					<p class="text-red-300/70">{error}</p>
				</div>
			</div>
		</div>
	{:else if snippet}
		<div class="grid min-h-0 flex-1 grid-cols-1 gap-4 p-4 lg:grid-cols-5">
			<!-- LEFT: code (60%) -->
			<section class="glass flex min-h-0 flex-col overflow-hidden rounded-2xl lg:col-span-3">
				<div class="flex items-center justify-between gap-3 border-b border-white/5 px-4 py-2.5">
					<span class="text-xs text-slate-500">
						{codeView === 'code'
							? 'Click a line number to comment on it'
							: 'Side-by-side diff against the previous version'}
					</span>
					{#if hasDiff}
						<div class="flex gap-1 rounded-lg border border-white/10 bg-white/[0.03] p-0.5">
							<button
								type="button"
								onclick={() => (codeView = 'code')}
								class={cn(
									'inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition',
									codeView === 'code'
										? 'bg-aurora-grad/20 text-white'
										: 'text-slate-400 hover:text-slate-200'
								)}
							>
								<CodeIcon class="h-3.5 w-3.5" /> Code
							</button>
							<button
								type="button"
								onclick={() => (codeView = 'diff')}
								class={cn(
									'inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition',
									codeView === 'diff'
										? 'bg-aurora-grad/20 text-white'
										: 'text-slate-400 hover:text-slate-200'
								)}
							>
								<GitCompareArrows class="h-3.5 w-3.5" /> Diff
							</button>
						</div>
					{/if}
				</div>

				<div class="min-h-0 flex-1 overflow-auto">
					{#if codeView === 'diff' && snippet.previousVersion}
						<DiffView
							oldCode={snippet.previousVersion}
							newCode={snippet.code}
							filename={snippet.title}
						/>
					{:else}
						<CodeEditor
							value={snippet.code}
							language={snippet.language}
							onLineClick={handleLineClick}
							class="h-full"
						/>
					{/if}
				</div>
			</section>

			<!-- RIGHT: reviews + AI (40%) -->
			<section class="flex min-h-0 flex-col gap-3 lg:col-span-2">
				<Tabs {tabs} bind:value={activeTab} />

				{#if activeTab === 'reviews'}
					<div class="flex min-h-0 flex-1 flex-col gap-3">
						<ThreadedReviewList
							{reviews}
							typing={typingNames}
							onReply={handleReply}
						/>
						{#if composerError}
							<div class="flex items-center gap-2 px-1 text-xs text-red-400">
								<AlertTriangle class="h-3.5 w-3.5" />
								{composerError}
							</div>
						{/if}
						<ReviewComposer
							{activeLine}
							replyToName={replyTo?.author?.githubUsername ?? null}
							onSubmit={submitReview}
							onTyping={notifyTyping}
							onClearLine={() => (activeLine = null)}
							onClearReply={() => (replyTo = null)}
						/>
					</div>
				{:else}
					<AIReviewPanel {aiReview} pending={aiPending} />
				{/if}
			</section>
		</div>
	{/if}
</div>
