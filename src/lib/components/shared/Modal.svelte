<script lang="ts">
	/**
	 * Reusable modal component with overlay, escape key handling, and click-outside-to-close.
	 *
	 * Usage:
	 * ```svelte
	 * <Modal bind:open={showModal} title="My Modal">
	 *   <p>Modal content here</p>
	 *   {#snippet footer()}
	 *     <button onclick={() => showModal = false}>Close</button>
	 *   {/snippet}
	 * </Modal>
	 * ```
	 */

	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		title = '',
		role = 'dialog' as 'dialog' | 'alertdialog',
		class: className = '',
		children,
		footer
	}: {
		open: boolean;
		title?: string;
		role?: 'dialog' | 'alertdialog';
		class?: string;
		children: Snippet;
		footer?: Snippet;
	} = $props();

	let modalRef = $state<HTMLDivElement | null>(null);

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.stopPropagation();
			open = false;
		}
	}

	function handleClickOutside(event: MouseEvent) {
		// Close if clicking outside the modal panel
		if (modalRef && !modalRef.contains(event.target as Node)) {
			open = false;
		}
	}

	$effect(() => {
		if (open) {
			// Use setTimeout to avoid immediately closing from the opening click
			const timer = setTimeout(() => {
				document.addEventListener('mousedown', handleClickOutside);
			}, 0);
			return () => {
				clearTimeout(timer);
				document.removeEventListener('mousedown', handleClickOutside);
			};
		}
	});
</script>

{#if open}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="modal-container" onkeydown={handleKeydown}>
		<div class="modal-backdrop" role="presentation"></div>
		<div
			class="modal {className}"
			{role}
			aria-modal="true"
			aria-labelledby={title ? 'modal-title' : undefined}
			bind:this={modalRef}
		>
		{#if title}
			<div class="modal-header">
				<h3 class="modal-title" id="modal-title">{title}</h3>
				<button class="btn-ghost icon-btn" onclick={() => (open = false)} aria-label="Close">
					<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
						<line x1="18" y1="6" x2="6" y2="18" />
						<line x1="6" y1="6" x2="18" y2="18" />
					</svg>
				</button>
			</div>
		{/if}

		<div class="modal-body">
			{@render children()}
		</div>

		{#if footer}
			<div class="modal-footer">
				{@render footer()}
			</div>
		{/if}
	</div>
	</div>
{/if}

<style>
	.modal-container {
		position: fixed;
		inset: 0;
		z-index: 1000;
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.modal-backdrop {
		position: absolute;
		inset: 0;
		background: rgba(0, 0, 0, 0.6);
		pointer-events: none;
		animation: fade-in 0.15s ease-out;
	}

	@keyframes fade-in {
		from {
			opacity: 0;
		}
		to {
			opacity: 1;
		}
	}

	.modal {
		position: relative;
		z-index: 1;
		background: var(--bg-secondary);
		border: 1px solid var(--border-default);
		border-radius: 12px;
		min-width: 320px;
		max-width: 90vw;
		max-height: 85vh;
		overflow: visible;
		display: flex;
		flex-direction: column;
		box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
		animation: fade-in 0.15s ease-out;
	}

	.modal-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 1rem 1.25rem;
		border-bottom: 1px solid var(--border-subtle);
	}

	.modal-title {
		margin: 0;
		font-size: 1rem;
		font-weight: 600;
		color: var(--text-heading);
	}

	.modal-body {
		padding: 1.25rem;
		overflow-y: auto;
		flex: 1;
	}

	.modal-footer {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
		padding: 1rem 1.25rem;
		border-top: 1px solid var(--border-subtle);
	}

	.icon-btn {
		padding: 0.25rem;
		border-radius: 4px;
	}
</style>
