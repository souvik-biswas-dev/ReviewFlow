import type { Extension } from '@codemirror/state';
import { StreamLanguage } from '@codemirror/language';
import { javascript } from '@codemirror/lang-javascript';
import { python } from '@codemirror/lang-python';
import { go } from '@codemirror/legacy-modes/mode/go';
import { rust } from '@codemirror/legacy-modes/mode/rust';
import { c, cpp, java, csharp } from '@codemirror/legacy-modes/mode/clike';
import { ruby } from '@codemirror/legacy-modes/mode/ruby';

/** Languages offered in the selector. */
export const LANGUAGES = [
	'go',
	'javascript',
	'typescript',
	'python',
	'rust',
	'java',
	'c',
	'cpp',
	'csharp',
	'ruby'
] as const;

export type Language = (typeof LANGUAGES)[number];

/** Human label for a language id. */
export function languageLabel(lang: string): string {
	const map: Record<string, string> = {
		go: 'Go',
		javascript: 'JavaScript',
		typescript: 'TypeScript',
		python: 'Python',
		rust: 'Rust',
		java: 'Java',
		c: 'C',
		cpp: 'C++',
		csharp: 'C#',
		ruby: 'Ruby'
	};
	return map[lang.toLowerCase()] ?? lang;
}

/** Returns the CodeMirror syntax extension for a language id. */
export function languageExtension(lang: string): Extension {
	switch (lang.toLowerCase()) {
		case 'javascript':
			return javascript();
		case 'typescript':
			return javascript({ typescript: true });
		case 'python':
			return python();
		case 'go':
			return StreamLanguage.define(go);
		case 'rust':
			return StreamLanguage.define(rust);
		case 'java':
			return StreamLanguage.define(java);
		case 'c':
			return StreamLanguage.define(c);
		case 'cpp':
			return StreamLanguage.define(cpp);
		case 'csharp':
			return StreamLanguage.define(csharp);
		case 'ruby':
			return StreamLanguage.define(ruby);
		default:
			return [];
	}
}
