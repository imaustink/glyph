<script lang="ts">
  import { uiStore } from '$lib/stores/ui.svelte';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { searchProvider as search } from '$lib/search/searchProvider';
  import type { SearchResult, SearchableItem } from '$lib/models/types';
  import { goto } from '$app/navigation';
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
    tasksStore.tasks;
    pagesStore.nodes;
    rebuildIndex().then(() => { indexVersion++; });
  });

  $effect(() => {
    // Re-run search when query changes or index is rebuilt
    indexVersion;
    if (!query.trim()) { results = []; loading = false; return; }
    if (searchTimer) clearTimeout(searchTimer);
    loading = true;
    searchTimer = setTimeout(async () => {
      results = await search.search(query, { limit: 40 });
      loading = false;
    }, 150);
  });

  function navigate(result: SearchResult) {
    if (result.type === 'page') goto(`/notes/${result.id}`);
    else goto(`/tasks/${result.id}`);
  }

  const pages = $derived(results.filter((r) => r.type === 'page'));
  const tasks = $derived(results.filter((r) => r.type === 'task'));
</script>

<div class="search-page">
  <div class="search-header">
    <h1 class="search-title">Search</h1>
  </div>

  <div class="search-bar-wrapper">
    <div class="search-bar">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="11" cy="11" r="8"/>
        <line x1="21" y1="21" x2="16.65" y2="16.65"/>
      </svg>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="search-input"
        bind:value={query}
        placeholder="Search pages and tasks…"
        autofocus
      />
      {#if loading}
        <span class="loading-dot"></span>
      {/if}
    </div>
  </div>

  <div class="results-area">
    {#if !query.trim()}
      <div class="empty-state">
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <circle cx="11" cy="11" r="8"/>
          <line x1="21" y1="21" x2="16.65" y2="16.65"/>
        </svg>
        <p>Search across all your pages and tasks.</p>
        <p class="hint">Also accessible via <kbd>⌘K</kbd> from anywhere.</p>
      </div>
    {:else if results.length === 0 && !loading}
      <div class="empty-state">
        <p>No results for "<strong>{query}</strong>".</p>
      </div>
    {:else}
      {#if pages.length > 0}
        <section class="result-section">
          <h2 class="section-title">Pages</h2>
          {#each pages as result}
            <button class="result-item" onclick={() => navigate(result)}>
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
                <polyline points="14 2 14 8 20 8"/>
              </svg>
              <span class="result-title">{result.title}</span>
            </button>
          {/each}
        </section>
      {/if}

      {#if tasks.length > 0}
        <section class="result-section">
          <h2 class="section-title">Tasks</h2>
          {#each tasks as result}
            <button class="result-item" onclick={() => navigate(result)}>
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
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
        </section>
      {/if}
    {/if}
  </div>
</div>

<style>
  .search-page {
    max-width: 720px;
    margin: 0 auto;
    padding: 32px 40px 60px;
    height: 100%;
    overflow-y: auto;
  }

  .search-header { margin-bottom: 20px; }

  .search-title {
    font-size: var(--font-size-xl);
    font-weight: 700;
    color: var(--text-heading);
    margin: 0;
  }

  .search-bar-wrapper { margin-bottom: 28px; }

  .search-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    padding: 10px 16px;
    color: var(--text-muted);
    transition: border-color var(--transition-fast);
  }
  .search-bar:focus-within { border-color: var(--accent); }

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
    width: 8px; height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: pulse 0.8s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.3; }
    50% { opacity: 1; }
  }

  .result-section { margin-bottom: 24px; }

  .section-title {
    font-size: var(--font-size-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
    margin: 0 0 8px;
  }

  .result-item {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    width: 100%;
    text-align: left;
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
    padding: 10px 14px;
    margin-bottom: 5px;
    cursor: pointer;
    color: var(--text-secondary);
    transition: background var(--transition-fast), border-color var(--transition-fast);
  }
  .result-item:hover { background: var(--bg-hover); border-color: var(--border-default); color: var(--text-primary); }

  .result-item svg { flex-shrink: 0; margin-top: 2px; }

  .result-text { display: flex; flex-direction: column; gap: 3px; min-width: 0; }

  .result-title { font-size: var(--font-size-sm); color: var(--text-primary); font-weight: 500; }

  .result-excerpt {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 60px 0;
    color: var(--text-muted);
    gap: 8px;
    text-align: center;
  }
  .empty-state svg { opacity: 0.3; margin-bottom: 8px; }
  .empty-state p { margin: 0; font-size: var(--font-size-sm); }
  .empty-state .hint { font-size: var(--font-size-xs); }

  kbd {
    font-size: 11px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: 3px;
    padding: 1px 5px;
    font-family: var(--font-sans);
  }

  /* ─── Mobile responsive ─────────────────────────────────────────────────── */
  @media (max-width: 768px) {
    .search-page {
      padding: 48px 16px 40px;
    }
  }
</style>
