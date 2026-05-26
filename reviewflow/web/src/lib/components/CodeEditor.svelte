<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { EditorState, Compartment } from '@codemirror/state';
	import {
		EditorView,
		lineNumbers,
		highlightActiveLine,
		highlightActiveLineGutter,
		drawSelection,
		keymap
	} from '@codemirror/view';
	import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
	import {
		syntaxHighlighting,
		defaultHighlightStyle,
		indentOnInput,
		bracketMatching
	} from '@codemirror/language';
	import { editorTheme } from '$lib/codemirror/theme';
	import { languageExtension } from '$lib/codemirror/languages';

	interface Props {
		value?: string;
		language?: string;
		editable?: boolean;
		/** Fired with the 1-based line number when a gutter line is clicked. */
		onLineClick?: (line: number) => void;
		class?: string;
	}

	let {
		value = $bindable(''),
		language = 'go',
		editable = false,
		onLineClick,
		class: klass = ''
	}: Props = $props();

	let host = $state<HTMLDivElement>();
	let view: EditorView | null = null;
	const langComp = new Compartment();
	const editComp = new Compartment();

	onMount(() => {
		const state = EditorState.create({
			doc: value,
			extensions: [
				lineNumbers({
					domEventHandlers: {
						mousedown: (v, line) => {
							if (!onLineClick) return false;
							onLineClick(v.state.doc.lineAt(line.from).number);
							return true;
						}
					}
				}),
				highlightActiveLine(),
				highlightActiveLineGutter(),
				drawSelection(),
				history(),
				indentOnInput(),
				bracketMatching(),
				syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
				keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
				langComp.of(languageExtension(language)),
				editComp.of([EditorView.editable.of(editable), EditorState.readOnly.of(!editable)]),
				editorTheme,
				EditorView.lineWrapping,
				EditorView.updateListener.of((u) => {
					if (u.docChanged && editable) value = u.state.doc.toString();
				})
			]
		});
		view = new EditorView({ state, parent: host });
	});

	onDestroy(() => view?.destroy());

	// Sync external value changes (e.g. snippet loaded after mount) into the doc.
	$effect(() => {
		const v = value;
		if (view && v !== view.state.doc.toString()) {
			view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v } });
		}
	});

	// Hot-swap the syntax mode when `language` changes (no rebuild).
	$effect(() => {
		const l = language;
		view?.dispatch({ effects: langComp.reconfigure(languageExtension(l)) });
	});

	// Toggle editable/read-only if the prop flips.
	$effect(() => {
		const e = editable;
		view?.dispatch({
			effects: editComp.reconfigure([
				EditorView.editable.of(e),
				EditorState.readOnly.of(!e)
			])
		});
	});
</script>

<div bind:this={host} class={klass}></div>
