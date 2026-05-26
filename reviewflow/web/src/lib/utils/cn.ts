/** Tiny conditional-classnames helper (no dependency needed). */
export type ClassValue = string | false | null | undefined;

export function cn(...values: ClassValue[]): string {
	return values.filter(Boolean).join(' ');
}

/**
 * Deterministic accent colour for an identity string, so each user gets a
 * stable gradient avatar/badge across the app.
 */
const ACCENTS = [
	'from-aurora-violet to-aurora-blue',
	'from-aurora-cyan to-aurora-blue',
	'from-aurora-pink to-aurora-violet',
	'from-aurora-lime to-aurora-cyan',
	'from-aurora-blue to-aurora-violet'
];

export function accentFor(seed: string): string {
	let hash = 0;
	for (let i = 0; i < seed.length; i++) hash = (hash * 31 + seed.charCodeAt(i)) | 0;
	return ACCENTS[Math.abs(hash) % ACCENTS.length];
}
