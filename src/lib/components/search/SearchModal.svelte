<script lang="ts">
  import { uiStore } from '$lib/stores/ui.svelte';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { searchProvider as search } from '$lib/search/searchProvider';
  import type { SearchResult, SearchableItem } from '$lib/models/types';
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';
  let query = $state('');
  let results = $state<SearchResult[]>([]);
  let loading = $state(false);
  let searchTimer: ReturnType<typeof setTimeout> | null = null;

  async function rebuildIndex() {
    const items: SearchableItem[] = [
      ...pagesStore.nodes
        .filter((n) => n.type === 'page')
        .map((n) => ({ id: n.id, type: 'page' as const, title: n.title, body: '', tags: n.tags })),
      ...tasksStore.tasks.map((t) => ({
        id: t.id,
        type: 'task' as const,
        title: t.title,
        body: t.description,
        tags: t.tags
      }))
    ];
    await search.index(items);
  }

  let indexVersion = $state(0);

  $effect(() => {
    // Re-index on every store change
    tasksStore.tasks;
    pagesStore.nodes;
    rebuildIndex().then(() => { indexVersion++; });
  });

  $effect(() => {
    // Re-run search when query changes or index is rebuilt
    indexVersion;
    if (!query.trim()) { results = []; return; }
    if (searchTimer) clearTimeout(searchTimer);
    loading = true;
    searchTimer = setTimeout(async () => {
      results = await search.search(query, { limit: 30 });
      loading = false;
    }, 120);
  });

  function navigate(result: SearchResult) {
    uiStore.closeSearch();
    query = '';
    if (result.type === 'page') goto(`/notes/${result.id}`);
    else goto(`/tasks/${result.id}`);
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') uiStore.closeSearch();
  }

  const pages = $derived(results.filter((r) => r.type === 'page'));
  const tasks = $derived(results.filter((r) => r.type === 'task'));
</script>

<div class="modal-backdrop" onclick={uiStore.closeSearch} role="presentation">
  <div
    class="search-panel"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
    role="dialog"
    aria-label="Search"
    tabindex="-1"
  >
    <div class="search-input-row">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="11" cy="11" r="8"/>
        <line x1="21" y1="21" x2="16.65" y2="16.65"/>
      </svg>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="search-input"
        bind:value={query}
        placeholder="Search pages and tasks…"
        onkeydown={handleKeydown}
        autofocus
      />
      {#if loading}
        <span class="loading-dot"></span>
      {/if}
      <kbd class="esc-hint">Esc</kbd>
    </div>

    {#if query.trim()}
      <div class="results-container">
        {#if results.length === 0 && !loading}
          <div class="no-results">No results for "{query}"</div>
        {:else}
          {#if pages.length > 0}
            <div class="result-group">
              <div class="group-label">Pages</div>
              {#each pages as result}
                <button class="result-item" onclick={() => navigate(result)}>
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
                    <polyline points="14 2 14 8 20 8"/>
                  </svg>
                  <span class="result-title">{result.title}</span>
                </button>
              {/each}
            </div>
          {/if}

          {#if tasks.length > 0}
            <div class="result-group">
              <div class="group-label">Tasks</div>
              {#each tasks as result}
                <button class="result-item" onclick={() => navigate(result)}>
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="9 11 12 14 22 4"/>
                    <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/>
                  </svg>
                  <div class="result-text">
                    <span class="result-title">{result.title}</span>
                    {#if result.excerpt}
                      <span class="result-excerpt">{result.excerpt}</span>
                    {/if}
                  </div>
                </button>
              {/each}
            </div>
          {/if}
        {/if}
      </div>
    {:else}
      <div class="search-hint">
        Start typing to search pages and tasks.
      </div>
    {/if}
  </div>
</div>

<style>
  .search-panel {
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    width: 560px;
    max-width: calc(100vw - 32px);
    max-height: 70vh;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .search-input-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 16px;
    border-bottom: 1px solid var(--border-subtle);
    color: var(--text-muted);
  }

  .search-input {
    flex: 1;
    background: none;
    border: none;
    font-size: var(--font-size-md);
    color: var(--text-primary);
    outline: none;
    padding: 0;
  }
  .search-input::placeholder { color: var(--text-muted); }

  .loading-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: pulse 0.8s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.3; }
    50% { opacity: 1; }
  }

  .esc-hint {
    font-size: 10px;
    color: var(--text-muted);
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: 3px;
    padding: 1px 5px;
    font-family: var(--font-sans);
    flex-shrink: 0;
  }

  .results-container {
    overflow-y: auto;
    flex: 1;
    padding: 8px;
  }

  .result-group { margin-bottom: 6px; }

  .group-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
    padding: 6px 10px 4px;
  }

  .result-item {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    padding: 8px 10px;
    border-radius: var(--radius-md);
    cursor: pointer;
    color: var(--text-secondary);
  }
  .result-item:hover { background: var(--bg-hover); color: var(--text-primary); }

  .result-item svg { flex-shrink: 0; margin-top: 2px; }

  .result-text { display: flex; flex-direction: column; gap: 2px; min-width: 0; }

  .result-title { font-size: var(--font-size-sm); color: var(--text-primary); }

  .result-excerpt {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .no-results {
    padding: 24px;
    text-align: center;
    font-size: var(--font-size-sm);
    color: var(--text-muted);
  }

  .search-hint {
    padding: 20px 16px;
    font-size: var(--font-size-sm);
    color: var(--text-muted);
    text-align: center;
  }
</style>
