<script lang="ts">
  import { repositories } from '$lib/storage/config';
  import type { Share, SharePermission, ShareResourceType } from '$lib/models/types';

  interface Props {
    resourceType: ShareResourceType;
    resourceId: string;
    onclose: () => void;
  }

  let { resourceType, resourceId, onclose }: Props = $props();

  const repo = repositories.shares;

  let shares = $state<Share[]>([]);
  let loading = $state(true);
  let error = $state('');

  // Invite by email
  let emailInput = $state('');
  let newPermission = $state<SharePermission>('viewer');
  let adding = $state(false);
  let addError = $state('');

  async function loadShares() {
    if (!repo) return;
    try {
      shares = await repo.list(resourceType, resourceId);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load shares';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    loadShares();
  });

  const emailValid = $derived(/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(emailInput.trim()));

  async function addShare() {
    if (!repo || !emailValid) return;
    addError = '';
    adding = true;
    try {
      const share = await repo.create(resourceType, resourceId, emailInput.trim(), newPermission);
      shares = [...shares, share];
      emailInput = '';
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to share';
      addError = msg.includes('no user found') ? 'No account found with that email.' : msg;
    } finally {
      adding = false;
    }
  }

  function handleEmailKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && emailValid && !adding) addShare();
  }

  async function updatePermission(shareId: string, permission: SharePermission) {
    if (!repo) return;
    try {
      const updated = await repo.updatePermission(shareId, permission);
      shares = shares.map((s) => (s.id === shareId ? updated : s));
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to update';
    }
  }

  async function removeShare(shareId: string) {
    if (!repo) return;
    try {
      await repo.delete(shareId);
      shares = shares.filter((s) => s.id !== shareId);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to remove';
    }
  }

  function displayName(user: { id: string; name: string | null; email: string | null }) {
    return user.name ?? user.email ?? user.id;
  }

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) onclose();
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose();
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="modal-backdrop" onclick={handleBackdropClick} onkeydown={handleKeydown} role="dialog" aria-modal="true" aria-label="Share" tabindex="-1">
  <div class="modal" aria-label="Share">
    <div class="modal-header">
      <h2>Share</h2>
      <button class="btn-ghost icon-btn" onclick={onclose} aria-label="Close">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
        </svg>
      </button>
    </div>

    <div class="modal-body">
      {#if !repo}
        <p class="empty-state">Sharing is only available in API mode.</p>
      {:else}
        <!-- Add person -->
        <div class="add-section">
          <div class="search-row">
            <input
              class="email-input"
              type="email"
              placeholder="Email address…"
              bind:value={emailInput}
              onkeydown={handleEmailKeydown}
              autocomplete="off"
            />
            <select class="perm-select" bind:value={newPermission}>
              <option value="viewer">Viewer</option>
              <option value="editor">Editor</option>
            </select>
            <button
              class="btn-primary"
              onclick={addShare}
              disabled={!emailValid || adding}
            >
              {adding ? '…' : 'Invite'}
            </button>
          </div>
          {#if addError}<p class="field-error">{addError}</p>{/if}
        </div>

        <!-- Current shares -->
        {#if loading}
          <p class="empty-state">Loading…</p>
        {:else if error}
          <p class="field-error">{error}</p>
        {:else if shares.length === 0}
          <p class="empty-state">Not shared with anyone yet.</p>
        {:else}
          <ul class="share-list">
            {#each shares as share (share.id)}
              <li class="share-row">
                <span class="user-avatar">{displayName(share.sharedWith)[0].toUpperCase()}</span>
                <div class="user-info">
                  <span class="user-name">{displayName(share.sharedWith)}</span>
                  {#if share.sharedWith.email && share.sharedWith.name}
                    <span class="user-email">{share.sharedWith.email}</span>
                  {/if}
                </div>
                <select
                  class="perm-select"
                  value={share.permission}
                  onchange={(e) => updatePermission(share.id, (e.target as HTMLSelectElement).value as SharePermission)}
                >
                  <option value="viewer">Viewer</option>
                  <option value="editor">Editor</option>
                </select>
                <button class="btn-ghost icon-btn remove-btn" onclick={() => removeShare(share.id)} aria-label="Remove">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                  </svg>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      {/if}
    </div>
  </div>
</div>

<style>
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 300;
  }

  .modal {
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-lg);
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
    width: 480px;
    max-width: calc(100vw - 32px);
    display: flex;
    flex-direction: column;
    max-height: 80vh;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 16px 20px 12px;
    border-bottom: 1px solid var(--border-subtle);
  }

  .modal-header h2 {
    font-size: var(--font-size-md);
    font-weight: 600;
    color: var(--text-heading);
    margin: 0;
  }

  .modal-body {
    padding: 16px 20px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .add-section {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .search-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .email-input {
    flex: 1;
    padding: 7px 10px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
  }
  .email-input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .perm-select {
    padding: 7px 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }

  .btn-primary {
    padding: 7px 14px;
    border-radius: var(--radius-md);
    background: var(--accent);
    color: white;
    font-size: var(--font-size-sm);
    font-weight: 500;
    border: none;
    cursor: pointer;
    white-space: nowrap;
    transition: opacity var(--transition-fast);
  }
  .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-primary:not(:disabled):hover { opacity: 0.9; }

  .share-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .share-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 0;
    border-bottom: 1px solid var(--border-subtle);
  }
  .share-row:last-child { border-bottom: none; }

  .user-avatar {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    background: var(--accent-muted);
    color: var(--accent);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: var(--font-size-sm);
    font-weight: 600;
    flex-shrink: 0;
  }

  .user-info {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .user-name {
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .user-email {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .remove-btn {
    color: var(--text-muted);
    flex-shrink: 0;
  }
  .remove-btn:hover { color: var(--text-danger, #f87171); }

  .empty-state {
    color: var(--text-muted);
    font-size: var(--font-size-sm);
    text-align: center;
    padding: 16px 0;
    margin: 0;
  }

  .field-error {
    color: var(--text-danger, #f87171);
    font-size: var(--font-size-xs);
    margin: 0;
  }

  .icon-btn {
    padding: 4px;
    border-radius: var(--radius-sm);
    line-height: 0;
  }

  .btn-ghost {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-muted);
  }
  .btn-ghost:hover { color: var(--text-primary); }
</style>
