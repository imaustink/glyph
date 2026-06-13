<script lang="ts">
	import { notificationsStore, type Notification } from '$lib/stores/notifications.svelte';

	const typeStyles: Record<Notification['type'], string> = {
		success: 'toast-success',
		error: 'toast-error',
		warning: 'toast-warning',
		info: 'toast-info'
	};

	const typeIcons: Record<Notification['type'], string> = {
		success: '✓',
		error: '✕',
		warning: '⚠',
		info: 'ℹ'
	};
</script>

{#if notificationsStore.notifications.length > 0}
	<div class="toast-container" role="region" aria-label="Notifications">
		{#each notificationsStore.notifications as notification (notification.id)}
			<div class="toast {typeStyles[notification.type]}" role="alert">
				<span class="toast-icon">{typeIcons[notification.type]}</span>
				<span class="toast-message">{notification.message}</span>
				{#if notification.dismissible}
					<button
						class="toast-dismiss"
						onclick={() => notificationsStore.dismiss(notification.id)}
						aria-label="Dismiss notification"
					>
						×
					</button>
				{/if}
			</div>
		{/each}
	</div>
{/if}

<style>
	.toast-container {
		position: fixed;
		bottom: 1rem;
		right: 1rem;
		z-index: 9999;
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		max-width: 24rem;
	}

	.toast {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.75rem 1rem;
		border-radius: 8px;
		background: var(--bg-secondary);
		border: 1px solid var(--border-default);
		box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
		animation: slide-in 0.2s ease-out;
	}

	@keyframes slide-in {
		from {
			opacity: 0;
			transform: translateX(100%);
		}
		to {
			opacity: 1;
			transform: translateX(0);
		}
	}

	.toast-icon {
		flex-shrink: 0;
		width: 1.25rem;
		height: 1.25rem;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: 50%;
		font-size: 0.75rem;
		font-weight: 600;
	}

	.toast-success .toast-icon {
		background: var(--status-done);
		color: var(--bg-primary);
	}

	.toast-error .toast-icon {
		background: var(--status-cancelled);
		color: var(--bg-primary);
	}

	.toast-warning .toast-icon {
		background: var(--priority-high);
		color: var(--bg-primary);
	}

	.toast-info .toast-icon {
		background: var(--accent);
		color: var(--bg-primary);
	}

	.toast-message {
		flex: 1;
		font-size: 0.875rem;
		color: var(--text-primary);
	}

	.toast-dismiss {
		flex-shrink: 0;
		background: none;
		border: none;
		color: var(--text-muted);
		cursor: pointer;
		padding: 0.25rem;
		font-size: 1.25rem;
		line-height: 1;
		opacity: 0.6;
		transition: opacity 0.15s;
	}

	.toast-dismiss:hover {
		opacity: 1;
	}
</style>
