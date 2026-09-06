<script lang="ts">
  import { page } from '$app/state';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { uiStore } from '$lib/stores/ui.svelte';
  import Editor from '$lib/components/editor/Editor.svelte';
  import TagInput from '$lib/components/shared/TagInput.svelte';
  import SaveIndicator from '$lib/components/shared/SaveIndicator.svelte';
  import ShareDialog from '$lib/components/shared/ShareDialog.svelte';
  import VisibilityPicker from '$lib/components/shared/VisibilityPicker.svelte';
  import { PRIORITY_OPTIONS } from '$lib/models/constants';
  import type { Priority } from '$lib/models/types';
  import { storageMode } from '$lib/storage/config';
  import { authStore } from '$lib/stores/auth.svelte';
  import { onMount } from 'svelte';

  let editorComponent = $state<ReturnType<typeof Editor> | null>(null);

  const pageId = $derived(page.params.pageId!);
  const node = $derived(pagesStore.getById(pageId));

  let editingTitle = $state(false);
  let titleValue = $state('');
  let editingTags = $state(false);
  let nodeTags = $state<string[]>([]);
  let showShareDialog = $state(false);

  $effect(() => {
    if (node) {
      uiStore.setCurrentPage(node.id);
      titleValue = node.title;
      nodeTags = [...node.tags];
    }
  });

  $effect(() => {
    if (uiStore.shouldFocusTitle && node) {
      editingTitle = true;
      uiStore.setShouldFocusTitle(false);
    }
  });

  async function commitTitle() {
    editingTitle = false;
    if (titleValue.trim() && node) {
      await pagesStore.updateNode(node.id, { title: titleValue.trim() });
    }
  }

  async function commitTags() {
    editingTags = false;
    if (node) {
      await pagesStore.updateNode(node.id, { tags: nodeTags });
    }
  }

  async function commitPriority(priority: Priority) {
    if (node) {
      await pagesStore.updateNode(node.id, { priority });
    }
  }

  function handleTitleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); commitTitle(); editorComponent?.focus(); }
    if (e.key === 'Escape') { editingTitle = false; titleValue = node?.title ?? ''; }
  }

  const allTags = $derived([...new Set(pagesStore.nodes.flatMap((n) => n.tags))]);

  async function handleVisibilityChange(orgId: string | null, isPrivate: boolean) {
    if (!node) return;
    await pagesStore.updateNode(node.id, { orgId, isPrivate });
  }
</script>

<div class="notes-page">
  {#if !node}
    <div class="not-found">
      <p>Page not found.</p>
    </div>
  {:else if node.type === 'folder'}
    <div class="folder-view">
      <h1>{node.title}</h1>
      <p class="muted">This is a folder. Pages inside it appear in the sidebar.</p>
    </div>
  {:else}
    <div class="page-header">
      <div class="title-row">
        {#if editingTitle}
          <!-- svelte-ignore a11y_autofocus -->
          <input
            class="title-edit"
            bind:value={titleValue}
            placeholder="Untitled"
            onblur={commitTitle}
            onkeydown={handleTitleKeydown}
            autofocus
          />
        {:else}
          <button class="page-title" class:placeholder={!node.title} onclick={() => editingTitle = true}>
            {node.title || 'Untitled'}
          </button>
        {/if}
        <SaveIndicator />
        {#if storageMode === 'api' && node.userId === authStore.userId}
          <VisibilityPicker
            orgId={node.orgId}
            isPrivate={node.isPrivate ?? true}
            onchange={handleVisibilityChange}
            onshare={() => showShareDialog = true}
          />
        {/if}
      </div>

      <div class="meta-row">
        <div class="tags-col">
          {#if editingTags}
            <div class="tags-edit-wrapper">
              <TagInput bind:tags={nodeTags} suggestions={allTags} />
              <button class="btn-ghost" onclick={commitTags}>Done</button>
            </div>
          {:else}
            <div class="tags-display" onclick={() => { editingTags = true; }} role="button" tabindex="0" onkeydown={(e) => e.key === 'Enter' && (editingTags = true)}>
              {#if node.tags.length === 0}
                <span class="add-tags-hint">+ Add tags</span>
              {:else}
                {#each node.tags as tag}
                  <span class="tag-pill">{tag}</span>
                {/each}
                <span class="add-tags-hint">+ Edit</span>
              {/if}
            </div>
          {/if}
        </div>

        <label class="priority-picker" title="Note priority — drives task-board ordering">
          <span class="priority-label">Priority</span>
          <select
            class="priority-select"
            value={node.priority ?? 'none'}
            onchange={(e) => commitPriority((e.target as HTMLSelectElement).value as Priority)}
          >
            {#each PRIORITY_OPTIONS as opt}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        </label>
      </div>
    </div>

    <div class="editor-area">
      <Editor {pageId} bind:this={editorComponent} />
    </div>
  {/if}
</div>

{#if showShareDialog && node}
  <ShareDialog
    resourceType="page"
    resourceId={node.id}
    onclose={() => showShareDialog = false}
  />
{/if}

<style>
  .notes-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }

  .page-header {
    padding: 32px 60px 0;
    max-width: 760px;
    margin: 0 auto;
    width: 100%;
  }

  .title-row {
    margin-bottom: 8px;
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .page-title {
    font-size: 2em;
    font-weight: 700;
    color: var(--text-heading);
    margin: 0;
    line-height: 1.2;
    cursor: text;
    border: none;
    border-bottom: 1px solid transparent;
    background: none;
    padding: 0;
    text-align: left;
    width: 100%;
    font-family: inherit;
    transition: border-color var(--transition-fast);
  }
  .page-title:hover { border-bottom-color: var(--border-default); }
  .page-title.placeholder { color: var(--text-muted); }

  .title-edit {
    width: 100%;
    font-size: 2em;
    font-weight: 700;
    background: none;
    border: none;
    border-bottom: 2px solid var(--accent);
    border-radius: 0;
    padding: 0;
    color: var(--text-heading);
    outline: none;
  }

  .meta-row {
    margin-bottom: 4px;
    min-height: 28px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .tags-col {
    flex: 1;
    min-width: 0;
  }

  .priority-picker {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .priority-label {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }

  .priority-select {
    background: var(--bg-secondary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    color: var(--text-primary);
    font-size: var(--font-size-xs);
    padding: 3px 6px;
    cursor: pointer;
  }
  .priority-select:hover { border-color: var(--border-strong); }
  .priority-select:focus { outline: none; border-color: var(--accent); }

  .tags-display {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    align-items: center;
    cursor: pointer;
    padding: 2px 0;
  }

  .add-tags-hint {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    cursor: pointer;
  }
  .add-tags-hint:hover { color: var(--text-secondary); }

  .tags-edit-wrapper {
    display: flex;
    gap: 8px;
    align-items: flex-start;
  }
  .tags-edit-wrapper :global(.tag-input-wrapper) { flex: 1; }

  .editor-area {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .not-found, .folder-view {
    padding: 60px;
    color: var(--text-secondary);
  }

  .muted { color: var(--text-muted); }

  /* ─── Mobile responsive ─────────────────────────────────────────────────── */
  @media (max-width: 768px) {
    .page-header {
      /* Clear the fixed hamburger button (top:10 + height:36 = 46px) */
      padding: 56px 16px 0;
      max-width: 100%;
    }

    .page-title {
      font-size: 1.5em;
    }

    .title-edit {
      font-size: 1.5em;
    }

    .not-found, .folder-view {
      padding: 48px 16px;
    }
  }
</style>
