<script lang="ts">
	import { accentFor, cn } from '$lib/utils/cn';

	interface Props {
		src?: string | null;
		name: string;
		size?: number;
		ring?: boolean;
		title?: string;
		class?: string;
	}

	let { src = null, name, size = 36, ring = false, title, class: klass = '' }: Props = $props();

	let failed = $state(false);
	const initials = $derived((name || '?').trim().slice(0, 2).toUpperCase());
</script>

<div
	class={cn(
		'relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full',
		ring && 'ring-2 ring-aurora-cyan/70 ring-offset-2 ring-offset-ink-900',
		klass
	)}
	style={`width:${size}px;height:${size}px;font-size:${Math.round(size * 0.4)}px`}
	title={title ?? name}
>
	{#if src && !failed}
		<img {src} alt={name} class="h-full w-full object-cover" onerror={() => (failed = true)} />
	{:else}
		<span
			class={cn(
				'flex h-full w-full items-center justify-center bg-gradient-to-br font-semibold text-ink-900',
				accentFor(name)
			)}
		>
			{initials}
		</span>
	{/if}
</div>
