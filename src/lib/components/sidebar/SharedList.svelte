<script lang="ts">
  import { page } from '$app/state';
  import type { SharedItem } from '$lib/models/types';

  let { items }: { items: SharedItem[] } = $props();

  function hrefFor(item: SharedItem): string {
    return item.resourceType === 'folder'
      ? `/tasks/folder/${item.resourceId}`
      : `/notes/${item.resourceId}`;
  }

  function sharedByLabel(item: SharedItem): string {
    const who = item.sharedBy.name || item.sharedBy.email;
    return who ? `Shared by ${who}` : 'Shared with you';
  }
</script>

<ul class="shared-list">
  {#each items as item (item.resourceType + ':' + item.resourceId)}
    {@const href = hrefFor(item)}
    <li>
      <a class="shared-row" class:active={page.url.pathname === href} {href} title={sharedByLabel(item)}>
        <span class="shared-icon">
          {#if item.resourceType === 'folder'}
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
            </svg>
          {:else}
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
              <polyline points="14 2 14 8 20 8" />
            </svg>
          {/if}
        </span>
        <span class="shared-text">
          <span class="shared-title">{item.title || 'Untitled'}</span>
          <span class="shared-sub">{sharedByLabel(item)}</span>
        </span>
        {#if item.permission === 'viewer'}
          <span class="shared-badge" title="View only">View</span>
        {/if}
      </a>
    </li>
  {/each}
</ul>

<style>
  .shared-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .shared-row {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 6px;
    border-radius: var(--radius-sm);
    text-decoration: none;
    transition: background var(--transition-fast);
  }
  .shared-row:hover { background: var(--bg-hover); text-decoration: none; }
  .shared-row.active { background: var(--accent-bg); }
  .shared-row.active .shared-title { color: var(--accent); }

  .shared-icon {
    color: var(--text-muted);
    line-height: 0;
    flex-shrink: 0;
  }

  .shared-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }

  .shared-title {
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .shared-row:hover .shared-title { color: var(--text-primary); }

  .shared-sub {
    font-size: 10px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .shared-badge {
    flex-shrink: 0;
    font-size: 9px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--text-muted);
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: 3px;
    padding: 1px 4px;
  }
</style>
