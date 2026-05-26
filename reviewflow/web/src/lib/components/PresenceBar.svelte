<script lang="ts">
	import Avatar from './ui/Avatar.svelte';
	import type { Presence } from '$lib/types';

	interface Props {
		viewers: Presence[];
		max?: number;
	}

	let { viewers, max = 5 }: Props = $props();

	const shown = $derived(viewers.slice(0, max));
	const overflow = $derived(Math.max(0, viewers.length - max));
</script>

<div class="flex items-center gap-3">
	<span class="relative flex h-2 w-2">
		<span class="absolute inline-flex h-full w-full animate-pulse-ring rounded-full bg-aurora-cyan"></span>
		<span class="relative inline-flex h-2 w-2 rounded-full bg-aurora-cyan"></span>
	</span>
	<span class="text-xs font-medium text-slate-400">
		{viewers.length}
		{viewers.length === 1 ? 'person' : 'people'} watching
	</span>

	<div class="flex -space-x-2">
		{#each shown as v (v.userId)}
			<div class="animate-pop-in rounded-full ring-2 ring-ink-900" title={v.username}>
				<Avatar name={v.username} size={30} />
			</div>
		{/each}
		{#if overflow > 0}
			<div
				class="flex h-[30px] w-[30px] items-center justify-center rounded-full bg-white/10 text-[11px] font-semibold text-slate-300 ring-2 ring-ink-900"
			>
				+{overflow}
			</div>
		{/if}
	</div>
</div>
