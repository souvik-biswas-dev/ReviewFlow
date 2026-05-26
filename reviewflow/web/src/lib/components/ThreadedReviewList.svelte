<script lang="ts">
	import { CornerDownRight, ChevronDown, ChevronRight, MessageSquareReply } from 'lucide-svelte';
	import Avatar from './ui/Avatar.svelte';
	import Badge from './ui/Badge.svelte';
	import { timeAgo } from '$lib/utils/timeAgo';
	import { cn } from '$lib/utils/cn';
	import type { Review } from '$lib/types';

	interface Props {
		reviews: Review[];
		typing?: string[];
		onReply?: (review: Review) => void;
	}

	let { reviews, typing = [], onReply }: Props = $props();

	// Build a parent → replies map and a top-level list. The backend flattens to
	// depth ≤ 1, so this is genuinely 2-tier rendering.
	const grouped = $derived.by(() => {
		const replies = new Map<string, Review[]>();
		const top: Review[] = [];
		for (const r of reviews) {
			if (r.parentReviewId) {
				const arr = replies.get(r.parentReviewId) ?? [];
				arr.push(r);
				replies.set(r.parentReviewId, arr);
			} else {
				top.push(r);
			}
		}
		// Orphaned replies (parent missing) get promoted to top-level so they
		// don't disappear from the UI.
		for (const r of reviews) {
			if (r.parentReviewId && !reviews.some((x) => x.id === r.parentReviewId)) {
				top.push(r);
			}
		}
		return { top, replies };
	});

	// Track which threads are expanded.
	let expanded = $state<Record<string, boolean>>({});
	function toggle(id: string): void {
		expanded = { ...expanded, [id]: !expanded[id] };
	}

	let scroller = $state<HTMLDivElement>();
	$effect(() => {
		void reviews.length;
		if (scroller) {
			requestAnimationFrame(() =>
				scroller?.scrollTo({ top: scroller.scrollHeight, behavior: 'smooth' })
			);
		}
	});
</script>

{#snippet reviewCard(r: Review, isReply: boolean)}
	<article
		class={cn(
			'glass animate-fade-up rounded-xl p-3.5',
			isReply && 'border-aurora-violet/30 bg-white/[0.04]'
		)}
	>
		<header class="flex items-center justify-between gap-2">
			<div class="flex items-center gap-2">
				{#if isReply}
					<CornerDownRight class="h-3.5 w-3.5 shrink-0 text-aurora-violet" />
				{/if}
				<Avatar src={r.author?.avatarUrl} name={r.author?.githubUsername ?? '?'} size={26} />
				<span class="text-sm font-medium text-slate-200">{r.author?.githubUsername}</span>
				{#if r.lineNumber != null}
					<Badge tone="cyan">L{r.lineNumber}</Badge>
				{/if}
			</div>
			<span class="text-xs text-slate-500">{timeAgo(r.createdAt)}</span>
		</header>
		<p class="mt-2 whitespace-pre-wrap text-sm leading-relaxed text-slate-300">{r.body}</p>
		{#if !isReply && onReply}
			<div class="mt-2 flex justify-end">
				<button
					type="button"
					class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-slate-500 transition hover:bg-white/[0.04] hover:text-aurora-cyan"
					onclick={() => onReply?.(r)}
				>
					<MessageSquareReply class="h-3.5 w-3.5" /> Reply
				</button>
			</div>
		{/if}
	</article>
{/snippet}

<div bind:this={scroller} class="flex-1 space-y-3 overflow-y-auto pr-1">
	{#if reviews.length === 0}
		<div
			class="flex h-full flex-col items-center justify-center gap-1 text-center text-sm text-slate-500"
		>
			<p class="font-medium text-slate-400">No reviews yet</p>
			<p>Be the first to leave feedback.</p>
		</div>
	{/if}

	{#each grouped.top as parent (parent.id)}
		{@const replies = grouped.replies.get(parent.id) ?? []}
		{@const isOpen = expanded[parent.id] !== false}
		<div class="space-y-2">
			{@render reviewCard(parent, false)}

			{#if replies.length > 0}
				<div class="ml-6 space-y-2 border-l-2 border-aurora-violet/20 pl-3">
					<button
						type="button"
						class="inline-flex items-center gap-1 text-xs text-slate-500 transition hover:text-slate-300"
						onclick={() => toggle(parent.id)}
					>
						{#if isOpen}
							<ChevronDown class="h-3.5 w-3.5" />
						{:else}
							<ChevronRight class="h-3.5 w-3.5" />
						{/if}
						{replies.length}
						{replies.length === 1 ? 'reply' : 'replies'}
					</button>
					{#if isOpen}
						{#each replies as reply (reply.id)}
							{@render reviewCard(reply, true)}
						{/each}
					{/if}
				</div>
			{/if}
		</div>
	{/each}

	{#if typing.length > 0}
		<div class="flex items-center gap-2 px-1 text-xs text-slate-400">
			<span class="flex gap-1">
				<span class="h-1.5 w-1.5 animate-bounce rounded-full bg-aurora-cyan [animation-delay:-0.2s]"></span>
				<span class="h-1.5 w-1.5 animate-bounce rounded-full bg-aurora-cyan [animation-delay:-0.1s]"></span>
				<span class="h-1.5 w-1.5 animate-bounce rounded-full bg-aurora-cyan"></span>
			</span>
			{typing.join(', ')}
			{typing.length === 1 ? 'is' : 'are'} typing…
		</div>
	{/if}
</div>
