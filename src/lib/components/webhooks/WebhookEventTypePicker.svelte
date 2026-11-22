<script lang="ts">
  import { WEBHOOK_EVENT_TYPES } from '$lib/models/types';
  import type { WebhookEventType } from '$lib/models/types';

  let { value = $bindable([]) }: { value: WebhookEventType[] } = $props();

  const groups: { label: string; types: WebhookEventType[] }[] = [
    { label: 'Notes', types: ['note.created', 'note.updated', 'note.deleted'] },
    { label: 'Tasks', types: ['task.created', 'task.updated', 'task.deleted'] }
  ];

  function toggle(type: WebhookEventType) {
    const next = new Set(value);
    if (next.has(type)) next.delete(type);
    else next.add(type);
    // Keep the canonical order so the saved list is stable.
    value = WEBHOOK_EVENT_TYPES.filter((t) => next.has(t));
  }
</script>

<div class="picker">
  {#each groups as group (group.label)}
    <fieldset>
      <legend>{group.label}</legend>
      {#each group.types as type (type)}
        <label class="option">
          <input type="checkbox" checked={value.includes(type)} onchange={() => toggle(type)} />
          <code>{type}</code>
        </label>
      {/each}
    </fieldset>
  {/each}
</div>

<style>
  .picker {
    display: flex;
    gap: 24px;
    flex-wrap: wrap;
  }

  fieldset {
    border: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  legend {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    margin-bottom: 4px;
    padding: 0;
  }

  .option {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    cursor: pointer;
    min-height: 28px;
  }

  code {
    font-size: var(--font-size-xs);
  }
</style>
