<script lang="ts">
  import type { TreeNode, NoteTemplate } from '$lib/models/types';
  import { templatesStore } from '$lib/stores/templates.svelte';
  import { storageMode } from '$lib/storage/config';
  import { authStore } from '$lib/stores/auth.svelte';

  let {
    node,
    pos,
    onrename,
    onnewchild,
    onnewfromtemplate,
    onshare,
    onvisibility,
    ondelete,
    onclose
  }: {
    node: TreeNode;
    pos: { x: number; y: number };
    onrename: () => void;
    onnewchild: () => void;
    onnewfromtemplate: (template: NoteTemplate) => void;
    onshare: () => void;
    onvisibility: () => void;
    ondelete: () => void;
    onclose: () => void;
  } = $props();

  let showTemplateSubmenu = $state(false);
  let menuEl = $state<HTMLDivElement | null>(null);
  // Null until measured; until then we render at the raw click position.
  let adjustedPos = $state<{ x: number; y: number } | null>(null);
  const displayPos = $derived(adjustedPos ?? pos);
  // The template submenu normally opens to the right; flip it left when the
  // menu sits close enough to the right edge that it would overflow.
  let submenuFlip = $state(false);

  // Keep the menu fully on screen: if the click was near the right/bottom edge
  // the raw coordinates would push it partly (or wholly) out of view.
  $effect(() => {
    if (!menuEl) return;
    const margin = 8;
    const rect = menuEl.getBoundingClientRect();
    let x = pos.x;
    let y = pos.y;
    if (x + rect.width > window.innerWidth - margin) {
      x = Math.max(margin, window.innerWidth - rect.width - margin);
    }
    if (y + rect.height > window.innerHeight - margin) {
      y = Math.max(margin, window.innerHeight - rect.height - margin);
    }
    adjustedPos = { x, y };
    submenuFlip = x + rect.width + 140 > window.innerWidth - margin;
  });

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      onclose();
      e.stopPropagation();
    }
  }
</script>

<svelte:window onclick={onclose} />

<div
  class="context-menu"
  bind:this={menuEl}
  style="top: {displayPos.y}px; left: {displayPos.x}px;"
  onclick={(e) => e.stopPropagation()}
  onkeydown={handleKeydown}
  role="menu"
  tabindex="-1"
>
  <button class="context-item" onclick={onrename}>Rename</button>
  {#if node.type === 'folder'}
    <button class="context-item" onclick={onnewchild}>New page inside</button>
    <div
      class="context-item-wrapper"
      role="menuitem"
      tabindex="-1"
      onmouseenter={() => (showTemplateSubmenu = true)}
      onmouseleave={() => (showTemplateSubmenu = false)}
    >
      <button class="context-item has-submenu">
        New from template
        <svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor">
          <path d="M3 2l4 3-4 3V2z" />
        </svg>
      </button>
      {#if showTemplateSubmenu}
        <div class="template-submenu" class:flip={submenuFlip}>
          {#each templatesStore.templates as template (template.id)}
            <button class="context-item" onclick={() => onnewfromtemplate(template)}>
              <span class="submenu-item-name">{template.name}</span>
              {#if template.isDefault}
                <span class="submenu-default-dot" title="Default"></span>
              {/if}
            </button>
          {/each}
        </div>
      {/if}
    </div>
    {#if storageMode === 'api'}
      <button class="context-item" onclick={onshare}>Share with people…</button>
      {#if node.userId === authStore.userId}
        <button class="context-item" onclick={onvisibility}>Set visibility…</button>
      {/if}
    {/if}
  {/if}
  <div class="context-divider"></div>
  <button class="context-item danger" onclick={ondelete}>Delete</button>
</div>

<style>
  .context-menu {
    position: fixed;
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    padding: 4px;
    z-index: 200;
    min-width: 160px;
  }

  .context-item {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    padding: 6px 10px;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }

  .context-item:hover { background: var(--bg-hover); }
  .context-item.danger { color: var(--priority-urgent); }
  .context-item.danger:hover { background: rgba(224, 108, 117, 0.1); }

  .context-divider {
    height: 1px;
    background: var(--border-subtle);
    margin: 4px 0;
  }

  .context-item-wrapper {
    position: relative;
  }

  .context-item.has-submenu {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .template-submenu {
    position: absolute;
    left: 100%;
    top: 0;
    z-index: 201;
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    min-width: 140px;
    padding: 4px;
  }

  /* Open to the left when the parent menu is near the right edge. */
  .template-submenu.flip {
    left: auto;
    right: 100%;
  }

  .submenu-item-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .submenu-default-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    margin-left: 8px;
    flex-shrink: 0;
  }
</style>
