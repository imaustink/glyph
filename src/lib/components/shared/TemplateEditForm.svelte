<script lang="ts">
  import { untrack } from 'svelte';
  import type { NoteTemplate, TodoTriggerMatchMode, TreeNode } from '$lib/models/types';
  import { DEFAULT_TODO_TRIGGER } from '$lib/models/types';
  import { TITLE_TOKENS, evaluateTitleTemplate } from '$lib/utils/titleTemplate';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import TemplateEditor from '$lib/components/editor/TemplateEditor.svelte';

  let {
    template = null,
    onsave,
    onback
  }: {
    template: NoteTemplate | null;
    onsave: (data: {
      name: string;
      content: string;
      titleTemplate: string;
      todoTrigger: { pattern: string; matchMode: TodoTriggerMatchMode; blockTypes: string[] };
      defaultFolderId: string | null;
    }) => void;
    onback: () => void;
  } = $props();

  const EMPTY_DOC = JSON.stringify({ type: 'doc', content: [{ type: 'paragraph' }] });

  // Snapshot props once on mount — intentional, not reactive
  const init = untrack(() => $state.snapshot(template));
  let editName = $state(init?.name ?? 'New Template');
  let editContent = $state(init?.content ?? EMPTY_DOC);
  let editTitleTemplate = $state(init?.titleTemplate ?? '');
  let editTodoPattern = $state(init?.todoTrigger?.pattern ?? DEFAULT_TODO_TRIGGER.pattern);
  let editTodoMatchMode = $state<TodoTriggerMatchMode>(init?.todoTrigger?.matchMode ?? DEFAULT_TODO_TRIGGER.matchMode);
  let editTodoBlockType = $state(init?.todoTrigger?.blockTypes[0] ?? 'heading');
  let editDefaultFolderId = $state<string | null>(init?.defaultFolderId ?? null);
  let titleExprInputEl = $state<HTMLInputElement | null>(null);

  const titlePreview = $derived(
    editTitleTemplate.trim() ? evaluateTitleTemplate(editTitleTemplate) : ''
  );

  const folders = $derived(pagesStore.nodes.filter((n: TreeNode) => n.type === 'folder'));

  function insertToken(token: string) {
    const start = titleExprInputEl?.selectionStart ?? editTitleTemplate.length;
    const end = titleExprInputEl?.selectionEnd ?? editTitleTemplate.length;
    editTitleTemplate = editTitleTemplate.slice(0, start) + token + editTitleTemplate.slice(end);
    setTimeout(() => {
      titleExprInputEl?.focus();
      const pos = start + token.length;
      titleExprInputEl?.setSelectionRange(pos, pos);
    }, 0);
  }

  function handleSave() {
    if (!editName.trim()) return;
    onsave({
      name: editName.trim(),
      content: editContent,
      titleTemplate: editTitleTemplate,
      todoTrigger: {
        pattern: editTodoPattern.trim() || 'TODO',
        matchMode: editTodoMatchMode,
        blockTypes: [editTodoBlockType]
      },
      defaultFolderId: editDefaultFolderId
    });
  }
</script>

<div class="edit-form">
  <div class="field">
    <label class="field-label" for="template-name">Name</label>
    <input id="template-name" bind:value={editName} placeholder="Template name" />
  </div>

  <div class="field">
    <label class="field-label" for="template-title-expr">Default title</label>
    <input
      id="template-title-expr"
      bind:this={titleExprInputEl}
      bind:value={editTitleTemplate}
      placeholder="Leave blank to let user type a title…"
    />
    <div class="token-chips">
      <span class="token-chips-label">Insert:</span>
      {#each TITLE_TOKENS as t}
        <button class="token-chip" onclick={() => insertToken(t.token)} title={t.description}>
          {t.label}
        </button>
      {/each}
    </div>
    {#if titlePreview}
      <span class="title-preview">Preview: <em>{titlePreview}</em></span>
    {/if}
  </div>

  <div class="field">
    <label class="field-label" for="template-default-folder">Default folder</label>
    <select
      id="template-default-folder"
      bind:value={editDefaultFolderId}
      class="folder-select"
    >
      <option value={null}>Root (no folder)</option>
      {#each folders as folder (folder.id)}
        <option value={folder.id}>{folder.title || 'Untitled folder'}</option>
      {/each}
    </select>
    <span class="field-hint">New notes from this template will be created in this folder.</span>
  </div>

  <div class="field">
    <span class="field-label">Smart list trigger</span>
    <div class="trigger-row">
      <input
        id="template-todo-pattern"
        bind:value={editTodoPattern}
        placeholder="TODO"
        class="trigger-pattern-input"
        aria-label="Pattern"
      />
      <div class="trigger-mode-toggle" role="group" aria-label="Match mode">
        <button
          class="mode-btn"
          class:active={editTodoMatchMode === 'exact'}
          onclick={() => (editTodoMatchMode = 'exact')}
          type="button"
        >Exact</button>
        <button
          class="mode-btn"
          class:active={editTodoMatchMode === 'regex'}
          onclick={() => (editTodoMatchMode = 'regex')}
          type="button"
        >Regex</button>
      </div>
    </div>
    <div class="trigger-block-row">
      <span class="trigger-block-label">Match in</span>
      <select bind:value={editTodoBlockType} class="trigger-block-select">
        <option value="heading">Heading</option>
        <option value="paragraph">Paragraph</option>
        <option value="any">Any block</option>
      </select>
    </div>
    <span class="field-hint">
      {#if editTodoMatchMode === 'exact'}
        Bullet lists under a <strong>{editTodoBlockType === 'any' ? 'block' : editTodoBlockType}</strong> with exactly "<em>{editTodoPattern || 'TODO'}</em>" will auto-create tasks (case-insensitive).
      {:else}
        Bullet lists under a <strong>{editTodoBlockType === 'any' ? 'block' : editTodoBlockType}</strong> whose text matches the regex <code>/{editTodoPattern || 'TODO'}/</code> will auto-create tasks.
      {/if}
    </span>
  </div>

  <div class="field">
    <span class="field-label">Content</span>
    {#key template?.id ?? 'new'}
      <TemplateEditor content={editContent} onchange={(json) => (editContent = json)} />
    {/key}
  </div>
</div>

<div class="modal-footer">
  <button class="btn-ghost" onclick={onback}>Back</button>
  <button class="btn-primary" onclick={handleSave} disabled={!editName.trim()}>Save</button>
</div>

<style>
  .edit-form {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
    margin-bottom: 16px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .field-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
  }

  .field input {
    padding: 8px 10px;
    background: var(--bg-primary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
  }

  .field input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .token-chips {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
  }

  .token-chips-label {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    margin-right: 2px;
    flex-shrink: 0;
  }

  .token-chip {
    font-size: 11px;
    font-family: var(--font-mono, monospace);
    padding: 2px 7px;
    border-radius: 3px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    color: var(--accent);
    cursor: pointer;
    transition: background var(--transition-fast), border-color var(--transition-fast);
    line-height: 1.6;
  }

  .token-chip:hover {
    background: var(--accent-bg);
    border-color: var(--accent-muted);
  }

  .title-preview {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }

  .title-preview em {
    font-style: normal;
    color: var(--text-secondary);
  }

  .field-hint {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }

  .field-hint strong {
    color: var(--text-secondary);
    font-weight: 600;
  }

  .field-hint em {
    color: var(--accent);
    font-style: normal;
  }

  .field-hint code {
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    color: var(--accent);
    background: var(--accent-bg);
    padding: 1px 4px;
    border-radius: 3px;
  }

  .trigger-row {
    display: flex;
    gap: 6px;
    align-items: center;
  }

  .trigger-pattern-input {
    flex: 1;
    min-width: 0;
    padding: 8px 10px;
    background: var(--bg-primary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    font-family: var(--font-mono, monospace);
  }

  .trigger-pattern-input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .trigger-mode-toggle {
    display: flex;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    overflow: hidden;
    flex-shrink: 0;
  }

  .mode-btn {
    padding: 6px 10px;
    font-size: var(--font-size-xs);
    font-weight: 500;
    background: var(--bg-primary);
    color: var(--text-muted);
    border: none;
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast);
    line-height: 1.4;
  }

  .mode-btn:first-child {
    border-right: 1px solid var(--border-default);
  }

  .mode-btn:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .mode-btn.active {
    background: var(--accent-bg);
    color: var(--accent);
  }

  .trigger-block-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .trigger-block-label {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .trigger-block-select {
    font-size: var(--font-size-xs);
    padding: 4px 8px;
    background: var(--bg-primary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    color: var(--text-secondary);
    cursor: pointer;
  }

  .trigger-block-select:focus {
    outline: none;
    border-color: var(--accent);
  }

  .folder-select {
    padding: 8px 10px;
    background: var(--bg-primary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }

  .folder-select:focus {
    outline: none;
    border-color: var(--accent);
  }

  .modal-footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding-top: 12px;
    border-top: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }
</style>
