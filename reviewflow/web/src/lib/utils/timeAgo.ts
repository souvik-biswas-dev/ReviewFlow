/** Compact "time ago" formatter, e.g. "3m", "5h", "2d", "just now". */
export function timeAgo(input: string | number | Date): string {
	const then = new Date(input).getTime();
	if (Number.isNaN(then)) return '';
	const seconds = Math.floor((Date.now() - then) / 1000);

	if (seconds < 5) return 'just now';
	if (seconds < 60) return `${seconds}s ago`;

	const minutes = Math.floor(seconds / 60);
	if (minutes < 60) return `${minutes}m ago`;

	const hours = Math.floor(minutes / 60);
	if (hours < 24) return `${hours}h ago`;

	const days = Math.floor(hours / 24);
	if (days < 7) return `${days}d ago`;

	const weeks = Math.floor(days / 7);
	if (weeks < 5) return `${weeks}w ago`;

	const months = Math.floor(days / 30);
	if (months < 12) return `${months}mo ago`;

	return `${Math.floor(days / 365)}y ago`;
}
