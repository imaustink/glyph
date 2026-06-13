<script lang="ts">
  import type { LinkMeta } from '$lib/models/types';

  let {
    link,
    onremove
  }: {
    link: LinkMeta;
    onremove?: () => void;
  } = $props();

  const displayUrl = $derived(() => {
    try {
      const u = new URL(link.url);
      return u.hostname + (u.pathname !== '/' ? u.pathname : '');
    } catch {
      return link.url;
    }
  });

  function handleImageError(e: Event) {
    (e.target as HTMLImageElement).style.display = 'none';
  }
</script>

<a href={link.url} target="_blank" rel="noopener noreferrer" class="link-preview" title={link.url}>
  {#if link.image}
    <div class="preview-image">
      <img src={link.image} alt="" onerror={handleImageError} />
    </div>
  {/if}
  <div class="preview-body">
    <div class="preview-header">
      {#if link.favicon}
        <img class="favicon" src={link.favicon} alt="" width="16" height="16" onerror={handleImageError} />
      {/if}
      {#if link.siteName}
        <span class="site-name">{link.siteName}</span>
      {/if}
    </div>
    {#if link.title}
      <div class="preview-title">{link.title}</div>
    {/if}
    {#if link.description}
      <div class="preview-desc">{link.description}</div>
    {/if}
    <div class="preview-url">{displayUrl()}</div>
  </div>
  {#if onremove}
    <button
      class="remove-btn"
      title="Remove link"
      onclick={(e) => { e.preventDefault(); e.stopPropagation(); onremove?.(); }}
    >
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
      </svg>
    </button>
  {/if}
</a>

<style>
  .link-preview {
    display: flex;
    flex-direction: column;
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
    overflow: hidden;
    text-decoration: none;
    color: inherit;
    transition: border-color var(--transition-fast);
    position: relative;
  }
  .link-preview:hover {
    border-color: var(--border-default);
  }

  .preview-image {
    width: 100%;
    max-height: 180px;
    overflow: hidden;
    border-bottom: 1px solid var(--border-subtle);
    background: var(--bg-tertiary);
  }
  .preview-image img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .preview-body {
    padding: 12px 14px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .preview-header {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .favicon {
    width: 16px;
    height: 16px;
    border-radius: 2px;
    flex-shrink: 0;
  }

  .site-name {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }

  .preview-title {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--text-primary);
    line-height: 1.3;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .preview-desc {
    font-size: var(--font-size-xs);
    color: var(--text-secondary);
    line-height: 1.4;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .preview-url {
    font-size: 11px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .remove-btn {
    position: absolute;
    top: 8px;
    right: 8px;
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm, 4px);
    width: 26px;
    height: 26px;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    color: var(--text-secondary);
    transition: color var(--transition-fast), background var(--transition-fast);
  }
  .remove-btn:hover {
    color: var(--priority-urgent);
    border-color: var(--priority-urgent);
    background: rgba(224, 108, 117, 0.1);
  }
</style>
