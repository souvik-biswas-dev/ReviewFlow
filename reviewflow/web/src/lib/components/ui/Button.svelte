<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils/cn';

	interface Props {
		variant?: 'aurora' | 'ghost';
		type?: 'button' | 'submit';
		disabled?: boolean;
		class?: string;
		onclick?: (e: MouseEvent) => void;
		children?: Snippet;
	}

	let {
		variant = 'aurora',
		type = 'button',
		disabled = false,
		class: klass = '',
		onclick,
		children
	}: Props = $props();
</script>

<button
	{type}
	{disabled}
	{onclick}
	class={cn(variant === 'aurora' ? 'btn-aurora' : 'btn-ghost', klass)}
>
	{@render children?.()}
</button>
