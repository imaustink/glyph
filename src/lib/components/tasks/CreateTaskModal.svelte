<script lang="ts">
  import { goto } from '$app/navigation';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import type { Priority } from '$lib/models/types';
  import Modal from '$lib/components/shared/Modal.svelte';
  import TagInput from '$lib/components/shared/TagInput.svelte';
  import DatePicker from '$lib/components/shared/DatePicker.svelte';

  let { open = $bindable(false) }: { open: boolean } = $props();

  let title = $state('');
  let priority = $state<Priority>('none');
  let dueDate = $state('');
  let tags = $state<string[]>([]);
  let noteQuery = $state('');
  let selectedNoteId = $state<string | null>(null);
  let showNoteSuggestions = $state(false);
  let saving = $state(false);
  let titleInputEl = $state<HTMLInputElement | null>(null);

  const allTags = $derived([...new Set(tasksStore.tasks.flatMap((t) => t.tags))]);

  const selectedNote = $derived(
    selectedNoteId ? pagesStore.getById(selectedNoteId) : undefined
  );

  const noteMatches = $derived.by(() => {
    const q = noteQuery.trim().toLowerCase();
    if (!q) return [];
    return pagesStore.nodes
      .filter((n) => n.type === 'page' && n.title.toLowerCase().includes(q))
      .slice(0, 8);
  });

  function reset() {
    title = '';
    priority = 'none';
    dueDate = '';
    tags = [];
    noteQuery = '';
    selectedNoteId = null;
    showNoteSuggestions = false;
  }

  $effect(() => {
    if (open) {
      reset();
      queueMicrotask(() => titleInputEl?.focus());
    }
  });

  function selectNote(e: MouseEvent, id: string) {
    // Stop propagation so this doesn't reach Modal's document-level
    // click-outside handler after this button is removed from the DOM.
    e.stopPropagation();
    selectedNoteId = id;
    noteQuery = '';
    showNoteSuggestions = false;
  }

  function clearNote() {
    selectedNoteId = null;
    noteQuery = '';
  }

  async function handleCreate() {
    const trimmed = title.trim();
    if (!trimmed || saving) return;

    saving = true;
    try {
      const task = await tasksStore.createTask({
        title: trimmed,
        priority,
        dueDate: dueDate || null,
        tags: [...tags],
        sourcePageId: selectedNoteId ?? undefined
      });
      open = false;
      goto(`/tasks/${task.id}`);
    } catch {
      notificationsStore.error('Failed to create task.');
    } finally {
      saving = false;
    }
  }

  function handleTitleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      void handleCreate();
    }
  }
</script>

<Modal bind:open title="New task">
  <div class="field">
    <input
      class="title-input"
      bind:this={titleInputEl}
      bind:value={title}
      onkeydown={handleTitleKeydown}
      placeholder="Task title…"
    />
  </div>

  <div class="field-row">
    <label class="field-label" for="new-task-priority">Priority</label>
    <select id="new-task-priority" bind:value={priority}>
      <option value="none">None</option>
      <option value="low">Low</option>
      <option value="medium">Medium</option>
      <option value="high">High</option>
      <option value="urgent">Urgent</option>
    </select>
  </div>

  <div class="field-row">
    <span class="field-label">Due date</span>
    <DatePicker value={dueDate || null} onchange={(v) => (dueDate = v ?? '')} />
  </div>

  <div class="field-row">
    <span class="field-label">Tags</span>
    <TagInput bind:tags suggestions={allTags} />
  </div>

  <div class="field-row">
    <span class="field-label">Note</span>
    <div class="note-attach">
      {#if selectedNote}
        <span class="note-chip">
          {selectedNote.title || 'Untitled'}
          <button class="remove-note" onclick={clearNote} type="button" aria-label="Remove attached note">×</button>
        </span>
      {:else}
        <input
          class="note-search-input"
          bind:value={noteQuery}
          onfocus={() => (showNoteSuggestions = true)}
          onblur={() => setTimeout(() => (showNoteSuggestions = false), 150)}
          placeholder="Attach to a note (optional)…"
        />
        {#if showNoteSuggestions && noteMatches.length > 0}
          <div class="note-suggestions">
            {#each noteMatches as note (note.id)}
              <button class="note-suggestion-item" onmousedown={(e) => selectNote(e, note.id)} type="button">
                {note.title || 'Untitled'}
              </button>
            {/each}
          </div>
        {/if}
      {/if}
    </div>
  </div>

  {#snippet footer()}
    <button class="btn-ghost" onclick={() => (open = false)} type="button">Cancel</button>
    <button class="btn-primary" onclick={handleCreate} disabled={!title.trim() || saving} type="button">
      {saving ? 'Creating…' : 'Create task'}
    </button>
  {/snippet}
</Modal>

<style>
  .field { margin-bottom: 14px; }

  .title-input {
    width: 100%;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    font-size: var(--font-size-md);
    padding: 8px 12px;
    color: var(--text-primary);
  }
  .title-input:focus { border-color: var(--accent); outline: none; }

  .field-row {
    display: grid;
    grid-template-columns: 80px 1fr;
    align-items: start;
    gap: 10px;
    margin-bottom: 12px;
  }

  .field-label {
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
    text-align: right;
    padding-top: 6px;
  }

  .note-attach {
    position: relative;
    min-width: 0;
  }

  .note-search-input {
    width: 100%;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    font-size: var(--font-size-sm);
    padding: 6px 10px;
    color: var(--text-primary);
  }
  .note-search-input:focus { border-color: var(--accent); outline: none; }

  .note-chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    padding: 5px 10px;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    max-width: 100%;
  }

  .remove-note {
    background: none;
    border: none;
    padding: 0;
    color: var(--text-muted);
    font-size: 14px;
    line-height: 1;
    cursor: pointer;
  }
  .remove-note:hover { color: var(--text-primary); }

  .note-suggestions {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    right: 0;
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    z-index: 10;
    overflow: hidden;
    max-height: 220px;
    overflow-y: auto;
  }

  .note-suggestion-item {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    padding: 6px 10px;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .note-suggestion-item:hover { background: var(--bg-hover); }
</style>
