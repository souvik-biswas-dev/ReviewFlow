<script lang="ts">
	import { ChevronDown, Check } from 'lucide-svelte';
	import { cn } from '$lib/utils/cn';

	interface Option {
		value: string;
		label: string;
	}

	interface Props {
		options: Option[];
		value?: string | null;
		placeholder?: string;
		onchange?: (value: string) => void;
		class?: string;
	}

	let {
		options,
		value = $bindable(null),
		placeholder = 'Select…',
		onchange,
		class: klass = ''
	}: Props = $props();

	let open = $state(false);
	let root = $state<HTMLDivElement>();

	const selected = $derived(options.find((o) => o.value === value) ?? null);

	function choose(v: string): void {
		value = v;
		onchange?.(v);
		open = false;
	}

	// Close on outside click — only while open, so the listener self-cleans.
	$effect(() => {
		if (!open) return;
		const handler = (e: MouseEvent) => {
			if (root && !root.contains(e.target as Node)) open = false;
		};
		window.addEventListener('click', handler);
		return () => window.removeEventListener('click', handler);
	});
</script>

<div bind:this={root} class={cn('relative', klass)}>
	<button
		type="button"
		onclick={() => (open = !open)}
		class="flex w-full items-center justify-between gap-2 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-slate-200 transition hover:border-white/20"
	>
		<span class={selected ? '' : 'text-slate-500'}>{selected?.label ?? placeholder}</span>
		<ChevronDown class={cn('h-4 w-4 text-slate-400 transition', open && 'rotate-180')} />
	</button>

	{#if open}
		<div
			class="glass absolute z-50 mt-2 max-h-64 w-full animate-fade-up overflow-auto rounded-xl p-1"
		>
			{#each options as opt (opt.value)}
				<button
					type="button"
					onclick={() => choose(opt.value)}
					class={cn(
						'flex w-full items-center justify-between rounded-lg px-3 py-2 text-left text-sm transition hover:bg-white/[0.06]',
						value === opt.value ? 'text-aurora-cyan' : 'text-slate-300'
					)}
				>
					{opt.label}
					{#if value === opt.value}<Check class="h-4 w-4" />{/if}
				</button>
			{/each}
		</div>
	{/if}
</div>
