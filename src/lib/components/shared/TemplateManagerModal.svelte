<script lang="ts">
  import { templatesStore } from '$lib/stores/templates.svelte';
  import type { NoteTemplate, TodoTriggerMatchMode } from '$lib/models/types';
  import ShareDialog from '$lib/components/shared/ShareDialog.svelte';
  import TemplateListView from './TemplateListView.svelte';
  import TemplateEditForm from './TemplateEditForm.svelte';

  let { onclose }: { onclose: () => void } = $props();

  type View = 'list' | 'edit';

  let view = $state<View>('list');
  let editingTemplate = $state<NoteTemplate | null>(null);
  let sharingTemplate = $state<NoteTemplate | null>(null);

  function startCreate() {
    editingTemplate = null;
    view = 'edit';
  }

  function startEdit(template: NoteTemplate) {
    editingTemplate = { ...template };
    view = 'edit';
  }

  async function handleSave(data: {
    name: string;
    content: string;
    titleTemplate: string;
    todoTrigger: { pattern: string; matchMode: TodoTriggerMatchMode; blockTypes: string[] };
    defaultFolderId: string | null;
  }) {
    if (editingTemplate) {
      await templatesStore.updateTemplate(editingTemplate.id, {
        name: data.name,
        content: data.content,
        titleTemplate: data.titleTemplate,
        todoTrigger: data.todoTrigger,
        defaultFolderId: data.defaultFolderId
      });
    } else {
      await templatesStore.createTemplate(
        data.name,
        data.content,
        data.titleTemplate,
        data.todoTrigger,
        data.defaultFolderId
      );
    }
    view = 'list';
  }

  function handleBackdropClick() {
    if (view === 'edit') {
      view = 'list';
    } else {
      onclose();
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="modal-backdrop" onclick={handleBackdropClick} role="presentation">
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="modal-panel templates-modal"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
    role="dialog"
    aria-label="Manage templates"
    tabindex="-1"
  >
    <div class="modal-header">
      <h2>{view === 'edit' ? (editingTemplate ? 'Edit Template' : 'New Template') : 'Templates'}</h2>
      <button class="btn-ghost icon-btn" onclick={onclose} aria-label="Close">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </div>

    {#if view === 'list'}
      <TemplateListView
        onedit={startEdit}
        oncreate={startCreate}
        onshare={(template) => sharingTemplate = template}
        {onclose}
      />
    {:else}
      <TemplateEditForm
        template={editingTemplate}
        onsave={handleSave}
        onback={() => view = 'list'}
      />
    {/if}
  </div>
</div>

{#if sharingTemplate}
  <ShareDialog
    resourceType="template"
    resourceId={sharingTemplate.id}
    onclose={() => sharingTemplate = null}
  />
{/if}

<style>
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .templates-modal {
    width: 520px;
    max-width: 92vw;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
    background: var(--bg-secondary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-lg);
    padding: 20px;
    box-shadow: var(--shadow-lg);
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 20px;
  }

  .modal-header h2 {
    margin: 0;
    font-size: var(--font-size-lg);
    color: var(--text-heading);
  }

  .icon-btn {
    padding: 4px;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
  }

  .icon-btn:hover {
    color: var(--text-primary);
  }
</style>
