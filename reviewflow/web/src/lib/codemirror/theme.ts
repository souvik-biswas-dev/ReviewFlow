import { EditorView } from '@codemirror/view';
import type { Extension } from '@codemirror/state';
import { oneDark } from '@codemirror/theme-one-dark';

/**
 * A translucent theme layered on top of one-dark so the editor blends into the
 * glassmorphic panel it sits inside, instead of painting an opaque rectangle.
 */
const glassTheme = EditorView.theme(
	{
		'&': {
			backgroundColor: 'transparent',
			color: '#e2e8f0'
		},
		'.cm-content': {
			caretColor: '#22d3ee',
			fontFamily: '"JetBrains Mono", ui-monospace, monospace'
		},
		'.cm-activeLine': { backgroundColor: 'rgba(124,92,255,0.07)' },
		'.cm-activeLineGutter': { backgroundColor: 'rgba(124,92,255,0.10)' },
		'.cm-selectionBackground, &.cm-focused .cm-selectionBackground': {
			backgroundColor: 'rgba(34,211,238,0.20)'
		},
		'.cm-scroller': { fontFamily: '"JetBrains Mono", ui-monospace, monospace' }
	},
	{ dark: true }
);

export const editorTheme: Extension = [oneDark, glassTheme];
