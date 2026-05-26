<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils/cn';

	interface Props {
		tone?: 'neutral' | 'violet' | 'cyan' | 'lime' | 'pink' | 'danger';
		class?: string;
		children?: Snippet;
	}

	let { tone = 'neutral', class: klass = '', children }: Props = $props();

	const tones: Record<NonNullable<Props['tone']>, string> = {
		neutral: 'border-white/10 bg-white/[0.04] text-slate-300',
		violet: 'border-aurora-violet/30 bg-aurora-violet/10 text-aurora-violet',
		cyan: 'border-aurora-cyan/30 bg-aurora-cyan/10 text-aurora-cyan',
		lime: 'border-aurora-lime/30 bg-aurora-lime/10 text-aurora-lime',
		pink: 'border-aurora-pink/30 bg-aurora-pink/10 text-aurora-pink',
		danger: 'border-red-500/40 bg-red-500/10 text-red-400'
	};
</script>

<span
	class={cn(
		'inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-xs font-medium',
		tones[tone],
		klass
	)}
>
	{@render children?.()}
</span>
