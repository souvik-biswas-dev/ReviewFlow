<script lang="ts">
	import { Sparkles, Gauge, Lightbulb, Wrench, ShieldAlert, ShieldCheck, ChevronDown } from 'lucide-svelte';
	import Badge from './ui/Badge.svelte';
	import { cn } from '$lib/utils/cn';
	import type { AIReview } from '$lib/types';

	interface Props {
		aiReview: AIReview | null;
		pending?: boolean;
	}

	let { aiReview = null, pending = false }: Props = $props();
	let showHints = $state(true);

	function scoreColor(s: number): string {
		if (s <= 3) return 'text-red-400 border-red-500/40 bg-red-500/10';
		if (s <= 6) return 'text-aurora-pink border-aurora-pink/40 bg-aurora-pink/10';
		if (s <= 8) return 'text-aurora-cyan border-aurora-cyan/40 bg-aurora-cyan/10';
		return 'text-aurora-lime border-aurora-lime/40 bg-aurora-lime/10';
	}
</script>

<div class="flex-1 space-y-4 overflow-y-auto pr-1">
	{#if !aiReview && pending}
		<!-- Analyzing skeleton -->
		<div class="glass flex items-center gap-3 rounded-xl p-4">
			<div class="relative">
				<Sparkles class="h-5 w-5 animate-pulse text-aurora-violet" />
			</div>
			<div>
				<p class="text-sm font-medium text-slate-200">Gemini is analyzing…</p>
				<p class="text-xs text-slate-500">This usually takes a few seconds.</p>
			</div>
		</div>
		<div class="space-y-3">
			<div class="skeleton h-20"></div>
			<div class="skeleton h-32"></div>
			<div class="skeleton h-24"></div>
		</div>
	{:else if !aiReview}
		<!-- No review and not pending -->
		<div class="flex h-full flex-col items-center justify-center gap-2 text-center text-sm text-slate-500">
			<Sparkles class="h-6 w-6 text-slate-600" />
			<p class="font-medium text-slate-400">No AI review available</p>
			<p>It may still be queued, or AI is disabled on the server.</p>
		</div>
	{:else}
		<!-- Score header -->
		<div class="glass flex items-center justify-between rounded-xl p-4">
			<div>
				<p class="text-xs uppercase tracking-wide text-slate-500">Quality score</p>
				<p class="mt-1 text-sm text-slate-300">Powered by Gemini</p>
			</div>
			<div
				class={cn(
					'flex h-16 w-16 flex-col items-center justify-center rounded-2xl border text-2xl font-bold',
					scoreColor(aiReview.qualityScore)
				)}
			>
				{aiReview.qualityScore}
				<span class="text-[10px] font-normal opacity-70">/ 10</span>
			</div>
		</div>

		<!-- Complexity -->
		<section class="glass rounded-xl p-4">
			<h4 class="flex items-center gap-2 text-sm font-semibold text-slate-200">
				<Gauge class="h-4 w-4 text-aurora-blue" /> Complexity
			</h4>
			<p class="mt-2 font-mono text-sm text-slate-300">{aiReview.complexity}</p>
		</section>

		<!-- Suggestions -->
		<section class="glass rounded-xl p-4">
			<h4 class="flex items-center gap-2 text-sm font-semibold text-slate-200">
				<Lightbulb class="h-4 w-4 text-aurora-lime" /> Suggestions
				<Badge>{aiReview.suggestions.length}</Badge>
			</h4>
			<ul class="mt-3 space-y-2">
				{#each aiReview.suggestions as s, i (i)}
					<li class="flex gap-2 text-sm leading-relaxed text-slate-300">
						<span class="mt-0.5 text-aurora-lime">▹</span>
						<span>{s}</span>
					</li>
				{:else}
					<li class="text-sm text-slate-500">No suggestions — clean code.</li>
				{/each}
			</ul>
		</section>

		<!-- Refactor hints (collapsible) -->
		<section class="glass rounded-xl p-4">
			<button
				type="button"
				class="flex w-full items-center justify-between"
				onclick={() => (showHints = !showHints)}
			>
				<h4 class="flex items-center gap-2 text-sm font-semibold text-slate-200">
					<Wrench class="h-4 w-4 text-aurora-violet" /> Refactor hints
					<Badge>{aiReview.refactorHints.length}</Badge>
				</h4>
				<ChevronDown class={cn('h-4 w-4 text-slate-400 transition', showHints && 'rotate-180')} />
			</button>
			{#if showHints}
				<ul class="mt-3 space-y-2">
					{#each aiReview.refactorHints as h, i (i)}
						<li class="flex gap-2 text-sm leading-relaxed text-slate-300">
							<span class="mt-0.5 text-aurora-violet">↻</span>
							<span>{h}</span>
						</li>
					{:else}
						<li class="text-sm text-slate-500">No refactors recommended.</li>
					{/each}
				</ul>
			{/if}
		</section>

		<!-- Security flags -->
		<section class="glass rounded-xl p-4">
			<h4 class="flex items-center gap-2 text-sm font-semibold text-slate-200">
				{#if aiReview.securityFlags.length > 0}
					<ShieldAlert class="h-4 w-4 text-red-400" /> Security flags
				{:else}
					<ShieldCheck class="h-4 w-4 text-aurora-lime" /> Security
				{/if}
			</h4>
			{#if aiReview.securityFlags.length > 0}
				<div class="mt-3 space-y-2">
					{#each aiReview.securityFlags as flag, i (i)}
						<div class="rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-300">
							{flag}
						</div>
					{/each}
				</div>
			{:else}
				<p class="mt-2 text-sm text-slate-400">No security issues detected.</p>
			{/if}
		</section>
	{/if}
</div>
