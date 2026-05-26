<script lang="ts">
	import { createTwoFilesPatch } from 'diff';
	import { html as diffHtml } from 'diff2html';
	import 'diff2html/bundles/css/diff2html.min.css';

	interface Props {
		oldCode: string;
		newCode: string;
		filename?: string;
		class?: string;
	}

	let { oldCode, newCode, filename = 'snippet', class: klass = '' }: Props = $props();

	// Build a unified patch on the fly and render with diff2html. We recompute
	// whenever inputs change.
	const rendered = $derived.by(() => {
		const patch = createTwoFilesPatch(
			`${filename} (previous)`,
			`${filename} (current)`,
			oldCode ?? '',
			newCode ?? ''
		);
		return diffHtml(patch, {
			drawFileList: false,
			matching: 'lines',
			outputFormat: 'side-by-side',
			renderNothingWhenEmpty: false
		});
	});
</script>

<!-- diff2html ships its own styles (loaded above); we host them inside a glass
     panel and gently invert the palette so it sits well on the dark theme. -->
<div class={`diff-view ${klass}`}>{@html rendered}</div>

<style>
	.diff-view :global(.d2h-wrapper) {
		background: transparent;
	}
	.diff-view :global(.d2h-file-header),
	.diff-view :global(.d2h-file-side-diff),
	.diff-view :global(.d2h-code-side-line),
	.diff-view :global(.d2h-code-line-ctn),
	.diff-view :global(.d2h-diff-tbody),
	.diff-view :global(.d2h-info) {
		background-color: rgba(255, 255, 255, 0.02) !important;
		color: #e2e8f0;
		border-color: rgba(255, 255, 255, 0.08) !important;
	}
	.diff-view :global(.d2h-code-side-linenumber) {
		background-color: rgba(255, 255, 255, 0.03) !important;
		color: rgba(148, 163, 184, 0.6);
		border-color: rgba(255, 255, 255, 0.06) !important;
	}
	.diff-view :global(.d2h-ins) {
		background-color: rgba(155, 255, 92, 0.10) !important;
	}
	.diff-view :global(.d2h-del) {
		background-color: rgba(255, 92, 200, 0.10) !important;
	}
	.diff-view :global(.d2h-cntx) {
		background-color: transparent !important;
	}
	.diff-view :global(.d2h-file-header) {
		background-color: rgba(255, 255, 255, 0.04) !important;
	}
</style>
