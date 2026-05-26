<script lang="ts">
	import Avatar from './ui/Avatar.svelte';
	import Badge from './ui/Badge.svelte';
	import { timeAgo } from '$lib/utils/timeAgo';
	import type { Review } from '$lib/types';

	interface Props {
		reviews: Review[];
		typing?: string[];
	}

	let { reviews, typing = [] }: Props = $props();

	let scroller = $state<HTMLDivElement>();

	// Auto-scroll to the newest review whenever the count changes.
	$effect(() => {
		void reviews.length;
		if (scroller) {
			requestAnimationFrame(() =>
				scroller?.scrollTo({ top: scroller.scrollHeight, behavior: 'smooth' })
			);
		}
	});
</script>

<div bind:this={scroller} class="flex-1 space-y-3 overflow-y-auto pr-1">
	{#if reviews.length === 0}
		<div class="flex h-full flex-col items-center justify-center gap-1 text-center text-sm text-slate-500">
			<p class="font-medium text-slate-400">No reviews yet</p>
			<p>Be the first to leave feedback.</p>
		</div>
	{/if}

	{#each reviews as r (r.id)}
		<article class="glass animate-fade-up rounded-xl p-3.5">
			<header class="flex items-center justify-between gap-2">
				<div class="flex items-center gap-2">
					<Avatar src={r.author?.avatarUrl} name={r.author?.githubUsername ?? '?'} size={26} />
					<span class="text-sm font-medium text-slate-200">{r.author?.githubUsername}</span>
					{#if r.lineNumber != null}
						<Badge tone="cyan">L{r.lineNumber}</Badge>
					{/if}
				</div>
				<span class="text-xs text-slate-500">{timeAgo(r.createdAt)}</span>
			</header>
			<p class="mt-2 whitespace-pre-wrap text-sm leading-relaxed text-slate-300">{r.body}</p>
		</article>
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
