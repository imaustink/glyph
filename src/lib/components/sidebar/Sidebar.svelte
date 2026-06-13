<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { uiStore } from '$lib/stores/ui.svelte';
  import { templatesStore } from '$lib/stores/templates.svelte';
  import { storageMode } from '$lib/storage/config';
  import type { NoteTemplate } from '$lib/models/types';
  import { evaluateTitleTemplate, evaluateContentTemplate } from '$lib/utils/titleTemplate';
  import PageTree from './PageTree.svelte';
  import TemplateManagerModal from '$lib/components/shared/TemplateManagerModal.svelte';

  let showTemplateDropdown = $state(false);
  let showTemplateManager = $state(false);

  async function handleNewPage() {
    const template = templatesStore.defaultTemplate;
    const title = template?.titleTemplate ? evaluateTitleTemplate(template.titleTemplate) : '';
    const content = template?.content ? evaluateContentTemplate(template.content) : undefined;
    const parentId = template?.defaultFolderId ?? null;
    const newPage = await pagesStore.createPage(parentId, title, content, template?.todoTrigger);
    uiStore.setShouldFocusTitle(true);
    goto(`/notes/${newPage.id}`);
  }

  async function handleNewPageFromTemplate(template: NoteTemplate) {
    showTemplateDropdown = false;
    const title = template.titleTemplate ? evaluateTitleTemplate(template.titleTemplate) : '';
    const content = evaluateContentTemplate(template.content);
    const parentId = template.defaultFolderId ?? null;
    const newPage = await pagesStore.createPage(parentId, title, content, template.todoTrigger);
    uiStore.setShouldFocusTitle(true);
    goto(`/notes/${newPage.id}`);
  }

  async function handleNewFolder() {
    await pagesStore.createFolder(null);
  }

  const isTasksActive = $derived(page.url.pathname.startsWith('/tasks'));
  const isSearchActive = $derived(page.url.pathname.startsWith('/search'));
  const isOrgsActive = $derived(page.url.pathname.startsWith('/settings/orgs'));

  /**
   * Pre-built parent→children index derived from the page tree.
   * Replaces per-level O(N) filter passes with O(1) map lookups.
   * Re-derived automatically whenever pagesStore.nodes changes.
   */
  const childrenByParent = $derived.by(() => {
    const m = new Map<string | null, (typeof pagesStore.nodes)[number][]>();
    for (const n of pagesStore.nodes) {
      if (!m.has(n.parentId)) m.set(n.parentId, []);
      m.get(n.parentId)!.push(n);
    }
    for (const arr of m.values()) {
      arr.sort((a, b) => a.order - b.order);
    }
    return m;
  });
</script>

<aside class="sidebar">
  <div class="sidebar-header">
    <span class="app-name">Glyph</span>
    <button class="btn-ghost icon-btn" onclick={uiStore.toggleSidebar} title="Toggle sidebar">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <rect x="3" y="3" width="18" height="18" rx="2"/>
        <line x1="9" y1="3" x2="9" y2="21"/>
      </svg>
    </button>
  </div>

  <nav class="sidebar-nav">
    <a href="/tasks" class="nav-item" class:active={isTasksActive}>
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <rect x="3" y="4" width="18" height="18" rx="2"/>
        <line x1="16" y1="2" x2="16" y2="6"/>
        <line x1="8" y1="2" x2="8" y2="6"/>
        <line x1="3" y1="10" x2="21" y2="10"/>
      </svg>
      Task Board
    </a>
    <a href="/search" class="nav-item" class:active={isSearchActive}>
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="11" cy="11" r="8"/>
        <line x1="21" y1="21" x2="16.65" y2="16.65"/>
      </svg>
      Search
      <kbd>⌘K</kbd>
    </a>
    {#if storageMode === 'api'}
      <a href="/settings/orgs" class="nav-item" class:active={isOrgsActive}>
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
          <circle cx="9" cy="7" r="4"/>
          <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
          <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
        </svg>
        Organizations
      </a>
    {/if}
  </nav>

  <div class="section-header">
    <span class="section-label">Pages</span>
    <div class="section-actions">
      <div
        class="new-page-btn-wrapper"
        role="group"
        aria-label="New page options"
        onmouseenter={() => (showTemplateDropdown = true)}
        onmouseleave={() => (showTemplateDropdown = false)}
      >
        <button class="btn-ghost icon-btn" onclick={handleNewPage} title="New page (default template)">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
            <polyline points="14 2 14 8 20 8"/>
            <line x1="12" y1="11" x2="12" y2="17"/>
            <line x1="9" y1="14" x2="15" y2="14"/>
          </svg>
        </button>

        {#if showTemplateDropdown}
          <div class="template-dropdown">
            {#each templatesStore.templates as template (template.id)}
              <button class="dropdown-item" onclick={() => handleNewPageFromTemplate(template)}>
                <span class="dropdown-item-name">{template.name}</span>
                {#if template.isDefault}
                  <span class="dropdown-default-dot" title="Default"></span>
                {/if}
              </button>
            {/each}
            <div class="dropdown-divider"></div>
            <button
              class="dropdown-item dropdown-manage"
              onclick={() => { showTemplateManager = true; showTemplateDropdown = false; }}
            >
              Manage templates
            </button>
          </div>
        {/if}
      </div>

      <button class="btn-ghost icon-btn" onclick={handleNewFolder} title="New folder">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
          <line x1="12" y1="11" x2="12" y2="17"/>
          <line x1="9" y1="14" x2="15" y2="14"/>
        </svg>
      </button>
    </div>
  </div>

  <div class="page-tree-container">
    <PageTree {childrenByParent} parentId={null} />
  </div>
</aside>

{#if showTemplateManager}
  <TemplateManagerModal onclose={() => (showTemplateManager = false)} />
{/if}

<style>
  .sidebar {
    width: var(--sidebar-width);
    min-width: var(--sidebar-width);
    background: var(--bg-secondary);
    border-right: 1px solid var(--border-subtle);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    height: 100vh;
  }

  .sidebar-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 12px 10px;
    border-bottom: 1px solid var(--border-subtle);
  }

  .app-name {
    font-size: var(--font-size-md);
    font-weight: 700;
    color: var(--text-heading);
    letter-spacing: -0.02em;
  }

  .icon-btn {
    padding: 4px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    line-height: 0;
  }
  .icon-btn:hover { color: var(--text-primary); }

  .sidebar-nav {
    padding: 8px 6px;
    border-bottom: 1px solid var(--border-subtle);
  }

  .nav-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border-radius: var(--radius-md);
    color: var(--text-secondary);
    font-size: var(--font-size-sm);
    text-decoration: none;
    transition: background var(--transition-fast), color var(--transition-fast);
  }
  .nav-item:hover { background: var(--bg-hover); color: var(--text-primary); text-decoration: none; }
  .nav-item.active { background: var(--accent-bg); color: var(--accent); }

  kbd {
    margin-left: auto;
    font-size: 10px;
    color: var(--text-muted);
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: 3px;
    padding: 1px 5px;
    font-family: var(--font-sans);
  }

  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 12px 4px;
  }

  .section-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
  }

  .section-actions {
    display: flex;
    align-items: center;
    gap: 2px;
  }

  .page-tree-container {
    flex: 1;
    overflow-y: auto;
    padding: 4px 6px 16px;
  }

  .new-page-btn-wrapper {
    position: relative;
  }

  .template-dropdown {
    position: absolute;
    top: calc(100% + 4px);
    right: 0;
    z-index: 200;
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    min-width: 160px;
    padding: 4px;
    display: flex;
    flex-direction: column;
  }

  .template-dropdown::before {
    content: '';
    position: absolute;
    top: -4px;
    left: 0;
    right: 0;
    height: 4px;
  }

  .dropdown-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 10px;
    border-radius: var(--radius-sm);
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
    background: none;
    border: none;
    cursor: pointer;
    text-align: left;
    width: 100%;
    transition: background var(--transition-fast), color var(--transition-fast);
  }

  .dropdown-item:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .dropdown-item-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .dropdown-default-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    flex-shrink: 0;
  }

  .dropdown-divider {
    height: 1px;
    background: var(--border-subtle);
    margin: 4px 0;
  }

  .dropdown-manage {
    color: var(--text-muted);
    font-size: var(--font-size-xs);
  }

  .dropdown-manage:hover {
    color: var(--text-secondary);
  }
</style>
