<script lang="ts">
  import type { NoteTemplate } from '$lib/models/types';
  import { templatesStore } from '$lib/stores/templates.svelte';
  import { storageMode } from '$lib/storage/config';
  import { authStore } from '$lib/stores/auth.svelte';
  import VisibilityPicker from './VisibilityPicker.svelte';

  let {
    onedit,
    oncreate,
    onshare,
    onclose
  }: {
    onedit: (template: NoteTemplate) => void;
    oncreate: () => void;
    onshare: (template: NoteTemplate) => void;
    onclose: () => void;
  } = $props();

  async function handleDelete(template: NoteTemplate) {
    await templatesStore.deleteTemplate(template.id);
  }

  async function handleSetDefault(template: NoteTemplate) {
    await templatesStore.setDefault(template.id);
  }

  async function handleVisibilityChange(template: NoteTemplate, newOrgId: string | null, newIsPrivate: boolean) {
    await templatesStore.updateTemplate(template.id, { orgId: newOrgId, isPrivate: newIsPrivate });
  }
</script>

<div class="templates-list">
  {#each templatesStore.templates as template (template.id)}
    <div class="template-row">
      <div class="template-info">
        <span class="template-name">{template.name}</span>
        {#if template.isDefault}
          <span class="badge-default">Default</span>
        {/if}
      </div>
      <div class="template-actions">
        {#if !template.isDefault}
          <button class="btn-ghost action-btn" onclick={() => handleSetDefault(template)}>
            Set default
          </button>
        {/if}
        {#if storageMode === 'api' && template.userId === authStore.userId}
          <VisibilityPicker
            orgId={template.orgId}
            isPrivate={template.isPrivate ?? true}
            onchange={(orgId, isPrivate) => handleVisibilityChange(template, orgId, isPrivate)}
            onshare={() => onshare(template)}
          />
        {/if}
        <button class="btn-ghost action-btn" onclick={() => onedit(template)}>
          Edit
        </button>
        {#if !(template.isDefault && templatesStore.templates.length === 1)}
          <button
            class="btn-ghost action-btn danger"
            onclick={() => handleDelete(template)}
            aria-label="Delete {template.name}"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
              <polyline points="3 6 5 6 21 6" />
              <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
              <path d="M10 11v6M14 11v6" />
              <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
            </svg>
          </button>
        {/if}
      </div>
    </div>
  {/each}
</div>

<div class="modal-footer">
  <button class="btn-ghost" onclick={onclose}>Close</button>
  <button class="btn-primary" onclick={oncreate}>+ New template</button>
</div>

<style>
  .templates-list {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 16px;
  }

  .template-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 12px;
    border-radius: var(--radius-md);
    background: var(--bg-tertiary);
    gap: 12px;
  }

  .template-info {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .template-name {
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .badge-default {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--accent);
    background: var(--accent-bg);
    border: 1px solid var(--accent-muted);
    border-radius: 3px;
    padding: 1px 6px;
    white-space: nowrap;
  }

  .template-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
  }

  .action-btn {
    font-size: var(--font-size-xs);
    padding: 3px 8px;
    border-radius: var(--radius-sm);
    color: var(--text-secondary);
    line-height: 1.4;
    white-space: nowrap;
  }

  .action-btn:hover {
    color: var(--text-primary);
    background: var(--bg-hover);
  }

  .action-btn.danger:hover {
    color: var(--status-cancelled);
    background: var(--bg-hover);
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
