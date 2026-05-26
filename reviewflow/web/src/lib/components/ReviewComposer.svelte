<script lang="ts">
	import { Send, X, CornerDownRight } from 'lucide-svelte';
	import Button from './ui/Button.svelte';
	import Badge from './ui/Badge.svelte';

	interface Props {
		activeLine?: number | null;
		/** Name of the review being replied to, if any. */
		replyToName?: string | null;
		onSubmit: (body: string, lineNumber: number | null) => Promise<void> | void;
		onTyping?: () => void;
		onClearLine?: () => void;
		onClearReply?: () => void;
	}

	let {
		activeLine = null,
		replyToName = null,
		onSubmit,
		onTyping,
		onClearLine,
		onClearReply
	}: Props = $props();

	let body = $state('');
	let submitting = $state(false);
	let lastTyping = 0;

	// Throttle typing notifications to at most one every 1.5s.
	function handleInput(): void {
		const now = Date.now();
		if (now - lastTyping > 1500) {
			lastTyping = now;
			onTyping?.();
		}
	}

	async function submit(): Promise<void> {
		const text = body.trim();
		if (!text || submitting) return;
		submitting = true;
		try {
			await onSubmit(text, activeLine);
			body = '';
			onClearLine?.();
			onClearReply?.();
		} finally {
			submitting = false;
		}
	}

	function onKeydown(e: KeyboardEvent): void {
		if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
			e.preventDefault();
			void submit();
		}
	}

	const placeholder = $derived(
		replyToName
			? `Reply to @${replyToName}…  (⌘/Ctrl + Enter to send)`
			: 'Share a review…  (⌘/Ctrl + Enter to send)'
	);
</script>

<div class="glass rounded-xl p-3">
	{#if activeLine != null || replyToName}
		<div class="mb-2 flex flex-wrap items-center gap-2">
			{#if replyToName}
				<Badge tone="violet">
					<CornerDownRight class="h-3 w-3" /> Replying to @{replyToName}
				</Badge>
				<button
					type="button"
					class="text-slate-400 transition hover:text-slate-200"
					onclick={() => onClearReply?.()}
					aria-label="Cancel reply"
				>
					<X class="h-4 w-4" />
				</button>
			{/if}
			{#if activeLine != null}
				<Badge tone="cyan">Commenting on line {activeLine}</Badge>
				<button
					type="button"
					class="text-slate-400 transition hover:text-slate-200"
					onclick={() => onClearLine?.()}
					aria-label="Clear line selection"
				>
					<X class="h-4 w-4" />
				</button>
			{/if}
		</div>
	{/if}

	<textarea
		bind:value={body}
		oninput={handleInput}
		onkeydown={onKeydown}
		rows="3"
		{placeholder}
		class="w-full resize-none rounded-lg border border-white/10 bg-ink-800/60 p-3 text-sm text-slate-200 placeholder:text-slate-500 focus:border-aurora-violet/50 focus:outline-none"
	></textarea>

	<div class="mt-2 flex justify-end">
		<Button onclick={submit} disabled={submitting || body.trim().length === 0}>
			<Send class="h-4 w-4" />
			{submitting ? 'Sending…' : replyToName ? 'Reply' : 'Add Review'}
		</Button>
	</div>
</div>
