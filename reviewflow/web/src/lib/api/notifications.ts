import { NOTIFICATIONS_URL } from '$lib/config';
import type { NotificationsResponse } from '$lib/types';

/** GET /notifications?page=N — paginated list + unreadCount. */
export async function fetchNotifications(page = 0): Promise<NotificationsResponse> {
	const res = await fetch(`${NOTIFICATIONS_URL}?page=${page}`, {
		credentials: 'include'
	});
	if (!res.ok) throw new Error(`notifications: HTTP ${res.status}`);
	return (await res.json()) as NotificationsResponse;
}

/** POST /notifications/read — mark every unread notification as read. */
export async function markAllRead(): Promise<void> {
	const res = await fetch(`${NOTIFICATIONS_URL}/read`, {
		method: 'POST',
		credentials: 'include'
	});
	if (!res.ok) throw new Error(`notifications/read: HTTP ${res.status}`);
}
