<script lang="ts">
  import { uiStore } from '$lib/stores/ui.svelte';

  const state = $derived(uiStore.saveState);
  const collab = $derived(uiStore.collabState);

  // Collaborative pages save continuously; show the connection instead.
  const collabLabel = $derived.by(() => {
    if (!collab) return null;
    if (collab.quarantined) return 'Read-only — recovering';
    if (collab.connection === 'connecting') return 'Connecting…';
    if (collab.connection === 'offline') return collab.unsynced ? 'Offline — changes will sync' : 'Offline';
    if (collab.readOnly) return 'Read-only';
    if (collab.unsynced) return 'Syncing…';
    return 'Saved';
  });
  const collabBusy = $derived(!!collab && (collab.connection !== 'connected' || collab.unsynced));
  const collabWarn = $derived(!!collab && (collab.connection === 'offline' || collab.quarantined));
</script>

{#if collab}
  <span
    class="save-indicator visible"
    class:warn={collabWarn}
    data-testid="collab-status"
    data-connection={collab.connection}
    data-unsynced={collab.unsynced}
    data-readonly={collab.readOnly}
    title="This note is edited live with anyone else who has it open."
  >
    <span class="icon">
      {#if collabBusy}
        <span class="dot"></span>
      {:else}
        <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
          <path d="M3 8.5L6.5 12L13 4" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
      {/if}
    </span>
    <span class="label">{collabLabel}</span>
  </span>
{:else}
  <span class="save-indicator" class:visible={state !== 'idle'}>
    <span class="icon">
      {#if state === 'saving'}
        <span class="dot"></span>
      {:else}
        <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
          <path d="M3 8.5L6.5 12L13 4" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
      {/if}
    </span>
    <span class="label">{state === 'saving' ? 'Saving…' : 'Saved'}</span>
  </span>
{/if}

<style>
  .save-indicator {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    user-select: none;
    opacity: 0;
    transition: opacity 0.25s ease;
    /* Reserve a fixed height so it never shifts layout */
    height: 1.4em;
  }

  .save-indicator.visible {
    opacity: 1;
  }

  .save-indicator.warn {
    color: var(--priority-high);
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 14px;
    height: 14px;
    flex-shrink: 0;
  }

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    animation: pulse 1s ease-in-out infinite;
  }

  .warn .dot {
    background: var(--priority-high);
  }

  .icon svg {
    color: var(--accent);
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }
</style>
