<script lang="ts">
  import { preferencesStore } from '$lib/stores/preferences.svelte';
  import { storageMode } from '$lib/storage/config';

  // URL previews fetch OG metadata from the server-side unfurl endpoint, which
  // only exists with the API backend.
  const urlPreviewsAvailable = storageMode === 'api';
</script>

<div class="preferences-page">
  <div class="page-header">
    <h1>Preferences</h1>
    <p class="subtitle">Display options for this device. They don't change your notes.</p>
  </div>

  <ul class="setting-list">
    <li class="setting-row">
      <div class="setting-text">
        <label class="setting-label" for="pref-url-previews">Show URL previews in notes</label>
        <p class="setting-desc">
          Render a preview card — title, description and image from a link's Open Graph
          metadata — beneath a URL that sits alone on its own line. Off by default; existing
          notes are unchanged until you turn it on.
          {#if !urlPreviewsAvailable}
            <br /><span class="muted">Available with the API backend. In local mode there is
            no server to fetch previews from.</span>
          {/if}
        </p>
      </div>
      <label class="switch" class:disabled={!urlPreviewsAvailable}>
        <input
          id="pref-url-previews"
          type="checkbox"
          role="switch"
          bind:checked={preferencesStore.urlPreviews}
          disabled={!urlPreviewsAvailable}
        />
        <span class="slider"></span>
      </label>
    </li>
  </ul>
</div>

<style>
  .preferences-page {
    padding: 32px 40px;
    max-width: 860px;
    margin: 0 auto;
  }

  .page-header {
    margin-bottom: 24px;
  }

  .page-header h1 {
    font-size: 1.6em;
    font-weight: 700;
    color: var(--text-heading);
    margin: 0 0 4px;
  }

  .subtitle {
    color: var(--text-secondary);
    margin: 0;
  }

  .setting-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .setting-row {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 24px;
    padding: 18px 0;
    border-bottom: 1px solid var(--border-subtle);
  }

  .setting-text {
    min-width: 0;
  }

  .setting-label {
    display: block;
    font-weight: 600;
    color: var(--text-primary);
    margin-bottom: 4px;
    cursor: pointer;
  }

  .setting-desc {
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
    margin: 0;
    line-height: 1.5;
  }

  .muted {
    color: var(--text-muted);
  }

  /* Toggle switch */
  .switch {
    position: relative;
    display: inline-block;
    width: 40px;
    height: 22px;
    flex-shrink: 0;
    margin-top: 2px;
  }
  .switch.disabled {
    opacity: 0.5;
  }
  .switch input {
    position: absolute;
    opacity: 0;
    width: 0;
    height: 0;
  }
  .slider {
    position: absolute;
    inset: 0;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: 999px;
    transition: background var(--transition-fast);
    cursor: pointer;
  }
  .switch.disabled .slider {
    cursor: not-allowed;
  }
  .slider::before {
    content: '';
    position: absolute;
    height: 16px;
    width: 16px;
    left: 2px;
    top: 2px;
    background: var(--text-secondary);
    border-radius: 50%;
    transition: transform var(--transition-fast), background var(--transition-fast);
  }
  .switch input:checked + .slider {
    background: var(--accent);
    border-color: var(--accent);
  }
  .switch input:checked + .slider::before {
    transform: translateX(18px);
    background: #fff;
  }
  .switch input:focus-visible + .slider {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
</style>
