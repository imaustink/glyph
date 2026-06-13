<script lang="ts">
  import { uiStore } from '$lib/stores/ui.svelte';

  const state = $derived(uiStore.saveState);
</script>

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

  .icon svg {
    color: var(--accent);
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.4; }
    50% { opacity: 1; }
  }
</style>
