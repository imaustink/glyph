<script lang="ts">
  import type { LinkMeta } from '$lib/models/types';

  let { link }: { link: LinkMeta } = $props();

  const displayHost = $derived(() => {
    try {
      return new URL(link.url).hostname;
    } catch {
      return link.url;
    }
  });

  function handleImageError(e: Event) {
    (e.target as HTMLImageElement).style.display = 'none';
  }
</script>

<a
  href={link.url}
  target="_blank"
  rel="noopener noreferrer"
  class="link-chip"
  title={link.title ?? link.url}
  onclick={(e) => e.stopPropagation()}
>
  {#if link.favicon}
    <img class="chip-favicon" src={link.favicon} alt="" width="12" height="12" onerror={handleImageError} />
  {:else}
    <svg class="chip-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/>
      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>
    </svg>
  {/if}
  <span class="chip-label">{link.siteName ?? displayHost()}</span>
</a>

<style>
  .link-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 7px;
    background: var(--bg-hover);
    border: 1px solid var(--border-subtle);
    border-radius: 10px;
    text-decoration: none;
    font-size: 11px;
    color: var(--text-secondary);
    max-width: 100%;
    overflow: hidden;
    transition: border-color var(--transition-fast), color var(--transition-fast);
  }
  .link-chip:hover {
    border-color: var(--accent);
    color: var(--accent);
  }

  .chip-favicon {
    width: 12px;
    height: 12px;
    border-radius: 2px;
    flex-shrink: 0;
  }

  .chip-icon {
    flex-shrink: 0;
  }

  .chip-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
