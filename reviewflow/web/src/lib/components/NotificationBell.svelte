<script lang="ts">
	import { onMount } from 'svelte';
	import { Bell, MessageSquare } from 'lucide-svelte';
	import { cn } from '$lib/utils/cn';
	import { timeAgo } from '$lib/utils/timeAgo';
	import { fetchNotifications, markAllRead } from '$lib/api/notifications';
	import type { NotificationItem } from '$lib/types';

	let open = $state(false);
	let items = $state<NotificationItem[]>([]);
	let unread = $state(0);
	let loading = $state(false);
	let root = $state<HTMLDivElement>();

	async function refresh(): Promise<void> {
		try {
			const res = await fetchNotifications();
			items = res.notifications;
			unread = res.unreadCount;
		} catch {
			// Bell stays empty on failure — better than crashing the dashboard.
		}
	}

	async function toggle(): Promise<void> {
		open = !open;
		if (open) {
			loading = true;
			await refresh();
			loading = false;
			if (unread > 0) {
				try {
					await markAllRead();
					unread = 0;
					items = items.map((n) => ({ ...n, read: true }));
				} catch {
					// non-fatal
				}
			}
		}
	}

	// Initial badge fetch on mount; light poll every 30s while page is open.
	onMount(() => {
		void refresh();
		const t = setInterval(refresh, 30_000);
		return () => clearInterval(t);
	});

	// Close on outside click while open.
	$effect(() => {
		if (!open) return;
		const handler = (e: MouseEvent) => {
			if (root && !root.contains(e.target as Node)) open = false;
		};
		window.addEventListener('click', handler);
		return () => window.removeEventListener('click', handler);
	});
</script>

<div bind:this={root} class="relative">
	<button
		type="button"
		onclick={toggle}
		class="relative inline-flex h-9 w-9 items-center justify-center rounded-lg text-slate-400 transition hover:bg-white/[0.05] hover:text-slate-200"
		aria-label="Notifications"
	>
		<Bell class="h-4 w-4" />
		{#if unread > 0}
			<span
				class="absolute right-1 top-1 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-aurora-grad px-1 text-[10px] font-bold text-ink-900"
			>
				{unread > 99 ? '99+' : unread}
			</span>
		{/if}
	</button>

	{#if open}
		<div
			class="glass absolute right-0 z-50 mt-2 w-80 animate-fade-up overflow-hidden rounded-xl"
		>
			<header class="border-b border-white/5 px-4 py-3 text-sm font-semibold text-slate-200">
				Notifications
			</header>
			<div class="max-h-96 overflow-y-auto">
				{#if loading}
					<div class="space-y-2 p-3">
						<div class="skeleton h-12 rounded-lg"></div>
						<div class="skeleton h-12 rounded-lg"></div>
					</div>
				{:else if items.length === 0}
					<div class="px-4 py-8 text-center text-sm text-slate-500">
						You're all caught up.
					</div>
				{:else}
					<ul class="divide-y divide-white/5">
						{#each items as n (n.id)}
							<li>
								<a
									href={`/snippet/${n.snippetId}`}
									class={cn(
										'flex items-start gap-3 px-4 py-3 text-sm transition hover:bg-white/[0.04]',
										!n.read && 'bg-aurora-violet/[0.06]'
									)}
								>
									<MessageSquare class="mt-0.5 h-4 w-4 shrink-0 text-aurora-cyan" />
									<div class="min-w-0">
										<p class="truncate text-slate-200">
											New review on <span class="font-medium">{n.snippetTitle}</span>
										</p>
										<p class="mt-0.5 text-xs text-slate-500">{timeAgo(n.createdAt)}</p>
									</div>
								</a>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</div>
	{/if}
</div>
