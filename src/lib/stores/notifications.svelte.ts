/**
 * Toast notification store for user feedback.
 *
 * Uses Svelte 5 runes. Notifications auto-dismiss after a timeout.
 */

import { uuid } from '$lib/utils/uuid';

export type NotificationType = 'success' | 'error' | 'warning' | 'info';

export interface Notification {
	id: string;
	type: NotificationType;
	message: string;
	dismissible: boolean;
}

const DISMISS_TIMEOUT = 5000;

export function createNotificationsStore() {
	let notifications = $state<Notification[]>([]);
	const timers = new Map<string, ReturnType<typeof setTimeout>>();

	function add(type: NotificationType, message: string, dismissible = true): string {
		const id = uuid();
		notifications = [...notifications, { id, type, message, dismissible }];

		if (dismissible) {
			const timer = setTimeout(() => dismiss(id), DISMISS_TIMEOUT);
			timers.set(id, timer);
		}

		return id;
	}

	function dismiss(id: string): void {
		const timer = timers.get(id);
		if (timer) {
			clearTimeout(timer);
			timers.delete(id);
		}
		notifications = notifications.filter((n) => n.id !== id);
	}

	function clear(): void {
		for (const timer of timers.values()) {
			clearTimeout(timer);
		}
		timers.clear();
		notifications = [];
	}

	return {
		get notifications() {
			return notifications;
		},

		success(message: string) {
			return add('success', message);
		},

		error(message: string) {
			return add('error', message);
		},

		warning(message: string) {
			return add('warning', message);
		},

		info(message: string) {
			return add('info', message);
		},

		/** Add a notification with explicit dismissible control. */
		add,
		dismiss,
		clear
	};
}

export const notificationsStore = createNotificationsStore();
