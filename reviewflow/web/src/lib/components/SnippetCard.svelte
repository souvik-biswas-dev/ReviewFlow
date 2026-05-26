<script lang="ts">
	import { Sparkles, MessageSquare } from 'lucide-svelte';
	import Badge from './ui/Badge.svelte';
	import Avatar from './ui/Avatar.svelte';
	import { languageLabel } from '$lib/codemirror/languages';
	import { timeAgo } from '$lib/utils/timeAgo';
	import type { SnippetCardData } from '$lib/types';

	interface Props {
		snippet: SnippetCardData;
	}

	let { snippet }: Props = $props();
</script>

<a href={`/snippet/${snippet.id}`} class="glass glass-hover group block rounded-2xl p-5">
	<div class="flex items-start justify-between gap-3">
		<h3
			class="line-clamp-2 font-display text-lg font-semibold text-slate-100 transition group-hover:text-aurora"
		>
			{snippet.title}
		</h3>
		{#if snippet.aiReview}
			<Badge tone="violet"><Sparkles class="h-3 w-3" /> AI</Badge>
		{/if}
	</div>

	<div class="mt-4 flex flex-wrap items-center gap-2">
		<Badge tone="cyan">{languageLabel(snippet.language)}</Badge>
		<Badge>
			<MessageSquare class="h-3 w-3" />
			{snippet.reviews.length}
			{snippet.reviews.length === 1 ? 'review' : 'reviews'}
		</Badge>
	</div>

	<div class="mt-5 flex items-center justify-between border-t border-white/5 pt-4">
		<div class="flex items-center gap-2">
			<Avatar src={snippet.author.avatarUrl} name={snippet.author.githubUsername} size={24} />
			<span class="text-xs text-slate-400">{snippet.author.githubUsername}</span>
		</div>
		<span class="text-xs text-slate-500">{timeAgo(snippet.createdAt)}</span>
	</div>
</a>
