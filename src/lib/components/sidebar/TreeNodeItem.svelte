<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import type { TreeNode, NoteTemplate, Task } from '$lib/models/types';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { uiStore } from '$lib/stores/ui.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import { evaluateTitleTemplate, evaluateContentTemplate } from '$lib/utils/titleTemplate';
  import ShareDialog from '$lib/components/shared/ShareDialog.svelte';
  import Modal from '$lib/components/shared/Modal.svelte';
  import VisibilityPicker from '$lib/components/shared/VisibilityPicker.svelte';
  import TreeNodeContextMenu from './TreeNodeContextMenu.svelte';
  import PageTree from './PageTree.svelte';

  let {
    node,
    childrenByParent
  }: {
    node: TreeNode;
    childrenByParent: Map<string | null, TreeNode[]>;
  } = $props();

  let expanded = $state(true);
  let editing = $state(false);
  let editTitle = $state('');
  let contextMenuOpen = $state(false);
  let contextMenuPos = $state({ x: 0, y: 0 });
  let showDeleteConfirm = $state(false);
  let associatedTaskCount = $state(0);
  let pendingDeleteTasks = $state<Task[]>([]);
  let dragOver = $state(false);
  let showShareDialog = $state(false);
  let showVisibilityModal = $state(false);

  async function handleFolderVisibilityChange(newOrgId: string | null, newIsPrivate: boolean) {
    try {
      await pagesStore.updateNode(node.id, { orgId: newOrgId, isPrivate: newIsPrivate });
    } catch {
      notificationsStore.error('Failed to update visibility.');
    }
  }

  const isActive = $derived(page.url.pathname === `/notes/${node.id}`);
  const hasChildren = $derived((childrenByParent.get(node.id)?.length ?? 0) > 0);

  function handleClick() {
    if (node.type === 'folder') {
      expanded = !expanded;
    } else {
      goto(`/notes/${node.id}`);
    }
  }

  function startEdit() {
    editing = true;
    editTitle = node.title;
    contextMenuOpen = false;
  }

  async function commitEdit() {
    if (editTitle.trim()) {
      try {
        await pagesStore.updateNode(node.id, { title: editTitle.trim() });
      } catch {
        notificationsStore.error('Failed to rename page.');
      }
    }
    editing = false;
  }

  function handleEditKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      commitEdit();
    }

    if (e.key === 'Escape') {
      editing = false;
    }
  }

  async function handleDelete() {
    contextMenuOpen = false;

    // Flatten the index into an array for the recursive task lookup.
    // This O(N) flatten only runs on user-initiated delete, not during render.
    const allNodes = [...childrenByParent.values()].flat();
    const associatedTasks = tasksStore.getByPageIdRecursive(node.id, allNodes);
    if (associatedTasks.length > 0) {
      associatedTaskCount = associatedTasks.length;
      pendingDeleteTasks = associatedTasks;
      showDeleteConfirm = true;
      return;
    }

    await confirmDelete(false);
  }

  async function confirmDelete(deleteAssociatedTasks: boolean) {
    showDeleteConfirm = false;

    if (deleteAssociatedTasks) {
      await Promise.all(pendingDeleteTasks.map(t => tasksStore.deleteTask(t.id)));
    }
    pendingDeleteTasks = [];

    await pagesStore.deleteNode(node.id);

    if (page.url.pathname === `/notes/${node.id}`) {
      const remaining = pagesStore.nodes.filter((n) => n.type === 'page' && n.id !== node.id);
      goto(remaining.length ? `/notes/${remaining[0].id}` : '/');
    }
  }

  async function handleNewChild() {
    contextMenuOpen = false;
    if (node.type === 'folder') {
      const child = await pagesStore.createPage(node.id);
      goto(`/notes/${child.id}`);
    }
  }

  function openContextMenu(e: MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    contextMenuPos = { x: e.clientX, y: e.clientY };
    contextMenuOpen = true;
  }

  async function handleNewChildFromTemplate(template: NoteTemplate) {
    contextMenuOpen = false;
    if (node.type === 'folder') {
      const title = template.titleTemplate ? evaluateTitleTemplate(template.titleTemplate) : '';
      const content = evaluateContentTemplate(template.content);
      const newPage = await pagesStore.createPage(node.id, title, content, template.todoTrigger);
      uiStore.setShouldFocusTitle(true);
      goto(`/notes/${newPage.id}`);
    }
  }

  function handleDragStart(e: DragEvent) {
    e.dataTransfer?.setData('text/plain', node.id);
    e.dataTransfer!.effectAllowed = 'move';
  }

  function handleDragOver(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
    const draggedId = e.dataTransfer?.types.includes('text/plain') ? true : false;
    if (!draggedId) return;
    e.dataTransfer!.dropEffect = 'move';
    dragOver = true;
  }

  function handleDragLeave(e: DragEvent) {
    // Only clear if actually leaving the node-row (not entering a child)
    const related = e.relatedTarget as HTMLElement | null;
    const currentTarget = e.currentTarget as HTMLElement;
    if (!related || !currentTarget.contains(related)) {
      dragOver = false;
    }
  }

  async function handleDrop(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
    dragOver = false;

    const draggedId = e.dataTransfer?.getData('text/plain');
    if (!draggedId || draggedId === node.id) return;

    // Don't allow dropping a node onto its own descendant
    function isDescendant(parentId: string, childId: string): boolean {
      const children = childrenByParent.get(parentId) ?? [];
      for (const c of children) {
        if (c.id === childId) return true;
        if (c.type === 'folder' && isDescendant(c.id, childId)) return true;
      }
      return false;
    }
    if (node.type === 'folder' && isDescendant(draggedId, node.id)) return;

    if (node.type === 'folder') {
      // Drop into folder — place at end
      const siblings = pagesStore.getChildren(node.id);
      const maxOrder = siblings.reduce((m, n) => Math.max(m, n.order), -1);
      await pagesStore.moveNode(draggedId, node.id, maxOrder + 1);
      expanded = true;
    } else {
      // Drop on a page/item — reorder as sibling (place after this node)
      const siblings = pagesStore.getChildren(node.parentId);
      const targetIndex = siblings.findIndex(n => n.id === node.id);
      // Reorder: shift everything after targetIndex up, insert dragged after target
      const newOrder = node.order + 0.5; // fractional, will be normalized
      await pagesStore.moveNode(draggedId, node.parentId, newOrder);
      // Normalize order for all siblings
      await normalizeOrder(node.parentId);
    }
  }

  async function normalizeOrder(parentId: string | null) {
    const siblings = pagesStore.getChildren(parentId);
    for (let i = 0; i < siblings.length; i++) {
      if (siblings[i].order !== i) {
        await pagesStore.moveNode(siblings[i].id, parentId, i);
      }
    }
  }
</script>

<div class="tree-node">
  <div
    class="node-row"
    class:active={isActive}
    class:folder={node.type === 'folder'}
    class:drag-over={dragOver}
    oncontextmenu={openContextMenu}
    draggable="true"
    ondragstart={handleDragStart}
    ondragover={handleDragOver}
    ondragleave={handleDragLeave}
    ondrop={handleDrop}
    role="treeitem"
    aria-selected={isActive}
    tabindex="-1"
  >
    {#if node.type === 'folder'}
      <button
        class="toggle-btn"
        onclick={() => expanded = !expanded}
        tabindex="-1"
        aria-label={expanded ? 'Collapse folder' : 'Expand folder'}
      >
        <svg
          width="10"
          height="10"
          viewBox="0 0 10 10"
          fill="currentColor"
          class:rotated={expanded}
        >
          <path d="M3 2l4 3-4 3V2z" />
        </svg>
      </button>
    {:else}
      <span class="node-icon">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <polyline points="14 2 14 8 20 8" />
        </svg>
      </span>
    {/if}

    {#if editing}
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="title-input"
        bind:value={editTitle}
        placeholder="Untitled"
        onblur={commitEdit}
        onkeydown={handleEditKeydown}
        autofocus
      />
    {:else}
      <button class="node-label" onclick={handleClick}>
        {node.title || 'Untitled'}
      </button>
    {/if}

    <div class="node-actions">
      {#if node.type === 'folder'}
        <button
          class="btn-ghost icon-btn"
          onclick={(e) => { e.stopPropagation(); goto(`/tasks/folder/${node.id}`); }}
          title="Open folder board"
        >
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/>
            <rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/>
          </svg>
        </button>
      {/if}
      <button class="btn-ghost icon-btn" onclick={openContextMenu} title="More options">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor">
          <circle cx="12" cy="5" r="1.5" />
          <circle cx="12" cy="12" r="1.5" />
          <circle cx="12" cy="19" r="1.5" />
        </svg>
      </button>
    </div>
  </div>

  {#if node.type === 'folder' && expanded && hasChildren}
    <div class="children">
      <PageTree {childrenByParent} parentId={node.id} />
    </div>
  {/if}
</div>

{#if contextMenuOpen}
  <TreeNodeContextMenu
    {node}
    pos={contextMenuPos}
    onrename={startEdit}
    onnewchild={handleNewChild}
    onnewfromtemplate={handleNewChildFromTemplate}
    onshare={() => { contextMenuOpen = false; showShareDialog = true; }}
    onvisibility={() => { contextMenuOpen = false; showVisibilityModal = true; }}
    ondelete={handleDelete}
    onclose={() => contextMenuOpen = false}
  />
{/if}

{#if showShareDialog}
  <ShareDialog
    resourceType="page"
    resourceId={node.id}
    onclose={() => showShareDialog = false}
  />
{/if}

<Modal bind:open={showVisibilityModal} title="Folder visibility" class="visibility-modal">
  <VisibilityPicker
    orgId={node.orgId}
    isPrivate={node.isPrivate ?? true}
    onchange={handleFolderVisibilityChange}
    onshare={() => { showVisibilityModal = false; showShareDialog = true; }}
  />
</Modal>

<Modal bind:open={showDeleteConfirm} title="Delete &quot;{node.title}&quot;?" role="alertdialog">
  <p class="modal-message">
    This note has <strong>{associatedTaskCount} associated task{associatedTaskCount !== 1 ? 's' : ''}</strong>.
  </p>
  <p class="modal-submessage">Would you like to delete them as well?</p>
  {#snippet footer()}
    <button class="btn-ghost" onclick={() => { showDeleteConfirm = false; }}>Cancel</button>
    <button class="btn-primary" onclick={() => confirmDelete(false)}>Keep Tasks</button>
    <button class="btn-danger" onclick={() => confirmDelete(true)}>Delete All</button>
  {/snippet}
</Modal>

<style>
  .tree-node { position: relative; }

  .node-row {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 3px 6px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: background var(--transition-fast);
    position: relative;
  }

  .node-row:hover { background: var(--bg-hover); }
  .node-row.active { background: var(--accent-bg); }
  .node-row.active .node-label { color: var(--accent); }
  .node-row:hover .node-actions { opacity: 1; }
  .node-row.drag-over { background: var(--accent-bg); outline: 1px solid var(--accent-muted); outline-offset: -1px; }
  .node-row.drag-over.folder { outline-style: dashed; }

  .toggle-btn {
    background: none;
    border: none;
    padding: 2px;
    color: var(--text-muted);
    line-height: 0;
    border-radius: 2px;
    flex-shrink: 0;
  }

  .toggle-btn svg { transition: transform var(--transition-fast); }
  .toggle-btn svg.rotated { transform: rotate(90deg); }

  .node-icon {
    color: var(--text-muted);
    line-height: 0;
    flex-shrink: 0;
  }

  .node-label {
    flex: 1;
    background: none;
    border: none;
    padding: 0;
    text-align: left;
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .node-row:hover .node-label { color: var(--text-primary); }

  .title-input {
    flex: 1;
    background: var(--bg-primary);
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    padding: 1px 6px;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    outline: none;
    min-width: 0;
  }

  .node-actions {
    opacity: 0;
    transition: opacity var(--transition-fast);
    margin-left: auto;
    flex-shrink: 0;
  }

  .icon-btn {
    padding: 2px;
    border-radius: 3px;
    color: var(--text-muted);
    line-height: 0;
  }

  .icon-btn:hover { color: var(--text-primary); }

  .children {
    padding-left: 14px;
  }

  /* Styles used by Modal children */
  .modal-message {
    margin: 0 0 8px 0;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
  }

  .modal-submessage {
    margin: 0 0 16px 0;
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
  }

  .btn-danger {
    background: var(--priority-urgent);
    color: white;
    border: none;
    padding: 6px 12px;
    border-radius: var(--radius-sm);
    font-size: var(--font-size-sm);
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .btn-danger:hover { background: #e07070; }
</style>
