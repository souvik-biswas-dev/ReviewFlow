<script lang="ts">
	import { cn } from '$lib/utils/cn';

	interface Tab {
		id: string;
		label: string;
		count?: number;
	}

	interface Props {
		tabs: Tab[];
		value: string;
		onchange?: (id: string) => void;
		class?: string;
	}

	let { tabs, value = $bindable(), onchange, class: klass = '' }: Props = $props();

	function select(id: string): void {
		value = id;
		onchange?.(id);
	}
</script>

<div class={cn('flex gap-1 rounded-xl border border-white/10 bg-white/[0.03] p-1', klass)}>
	{#each tabs as tab (tab.id)}
		<button
			type="button"
			onclick={() => select(tab.id)}
			class={cn(
				'relative flex-1 rounded-lg px-3 py-2 text-sm font-medium transition',
				value === tab.id ? 'text-white' : 'text-slate-400 hover:text-slate-200'
			)}
		>
			{#if value === tab.id}
				<span class="absolute inset-0 rounded-lg bg-aurora-grad opacity-20"></span>
				<span class="absolute inset-0 rounded-lg border border-aurora-violet/40"></span>
			{/if}
			<span class="relative z-10 inline-flex items-center justify-center gap-2">
				{tab.label}
				{#if tab.count !== undefined}
					<span class="rounded-full bg-white/10 px-1.5 py-0.5 text-[11px] leading-none">
						{tab.count}
					</span>
				{/if}
			</span>
		</button>
	{/each}
</div>
