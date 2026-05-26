<script lang="ts">
	import { goto } from '$app/navigation';
	import { getContextClient } from '@urql/svelte';
	import { ArrowLeft, AlertTriangle, Send } from 'lucide-svelte';
	import { CREATE_SNIPPET } from '$lib/graphql/snippets';
	import { LANGUAGES, languageLabel } from '$lib/codemirror/languages';
	import CodeEditor from '$lib/components/CodeEditor.svelte';
	import Select from '$lib/components/ui/Select.svelte';
	import Button from '$lib/components/ui/Button.svelte';

	const client = getContextClient();

	let title = $state('');
	let language = $state('go');
	let code = $state('');
	let previousVersion = $state('');
	let showPrevious = $state(false);
	let submitting = $state(false);
	let error = $state<string | null>(null);

	const langOptions = LANGUAGES.map((l) => ({ value: l, label: languageLabel(l) }));

	async function submit(): Promise<void> {
		if (!title.trim() || !code.trim() || !language) {
			error = 'Title, language and code are all required.';
			return;
		}
		submitting = true;
		error = null;

		const input: { title: string; language: string; code: string; previousVersion?: string } = {
			title: title.trim(),
			language,
			code
		};
		// Only include previousVersion if the user opted in and pasted something.
		if (showPrevious && previousVersion.trim().length > 0) {
			input.previousVersion = previousVersion;
		}

		const res = await client.mutation(CREATE_SNIPPET, { input }).toPromise();

		submitting = false;

		if (res.error || !res.data?.createSnippet) {
			error = res.error?.message ?? 'Failed to create snippet.';
			return;
		}
		await goto(`/snippet/${res.data.createSnippet.id}`);
	}
</script>

<svelte:head>
	<title>New Snippet · ReviewFlow</title>
</svelte:head>

<div class="mx-auto flex min-h-screen max-w-4xl flex-col px-6">
	<header class="flex items-center justify-between py-6">
		<a
			href="/dashboard"
			class="inline-flex items-center gap-2 text-sm text-slate-400 transition hover:text-slate-200"
		>
			<ArrowLeft class="h-4 w-4" /> Back to dashboard
		</a>
	</header>

	<main class="pb-16">
		<h1 class="font-display text-3xl font-bold text-slate-100">New snippet</h1>
		<p class="mt-2 text-sm text-slate-400">
			Post code for review — Gemini analyzes it automatically the moment you submit.
		</p>

		<div class="mt-8 space-y-5">
			<!-- Title -->
			<div>
				<label for="title" class="mb-1.5 block text-sm font-medium text-slate-300">Title</label>
				<input
					id="title"
					bind:value={title}
					placeholder="e.g. Binary search with duplicates"
					class="w-full rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2.5 text-slate-200 placeholder:text-slate-500 focus:border-aurora-violet/50 focus:outline-none"
				/>
			</div>

			<!-- Language -->
			<div class="max-w-xs">
				<label for="language" class="mb-1.5 block text-sm font-medium text-slate-300">Language</label
				>
				<Select bind:value={language} options={langOptions} />
			</div>

			<!-- Code editor -->
			<div>
				<span class="mb-1.5 block text-sm font-medium text-slate-300">Code</span>
				<div class="glass overflow-hidden rounded-xl">
					<CodeEditor bind:value={code} {language} editable class="h-[420px] overflow-auto" />
				</div>
			</div>

			<!-- Optional previous version (enables the diff viewer on the snippet page) -->
			<div>
				<label class="inline-flex cursor-pointer items-center gap-2 text-sm text-slate-300">
					<input
						type="checkbox"
						bind:checked={showPrevious}
						class="h-4 w-4 rounded border-white/20 bg-white/[0.04] text-aurora-violet focus:ring-aurora-violet/40"
					/>
					Include a previous version (enables side-by-side diff)
				</label>

				{#if showPrevious}
					<div class="glass mt-2 overflow-hidden rounded-xl">
						<CodeEditor
							bind:value={previousVersion}
							{language}
							editable
							class="h-[280px] overflow-auto"
						/>
					</div>
				{/if}
			</div>

			{#if error}
				<div class="flex items-center gap-2 text-sm text-red-400">
					<AlertTriangle class="h-4 w-4" />
					{error}
				</div>
			{/if}

			<div class="flex justify-end">
				<Button onclick={submit} disabled={submitting}>
					<Send class="h-4 w-4" />
					{submitting ? 'Creating…' : 'Create snippet'}
				</Button>
			</div>
		</div>
	</main>
</div>
