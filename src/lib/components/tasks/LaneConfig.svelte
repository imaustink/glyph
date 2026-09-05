<script lang="ts">
  import { untrack } from 'svelte';
  import { nanoid } from 'nanoid';
  import type { Lane, FilterRule, FilterSet, SortConfig, SortMode, SortDirection, TaskFilterField, Task } from '$lib/models/types';
  import { lanesStore } from '$lib/stores/lanes.svelte';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { notificationsStore } from '$lib/stores/notifications.svelte';

  let {
    lane,
    onclose,
    onupdate,
    ondelete
  }: {
    lane: Lane;
    onclose: () => void;
    /** Override the default lanesStore.updateLane call (used by folder board). */
    onupdate?: (id: string, patch: Partial<Omit<Lane, 'id'>>) => Promise<void>;
    /** Override the default lanesStore.deleteLane call (used by folder board). */
    ondelete?: (id: string) => Promise<void>;
  } = $props();

  // Snapshot props once on mount — intentional, not reactive
  // (the modal is destroyed/recreated when a different lane is configured)
  const init = untrack(() => $state.snapshot(lane));
  let title = $state(init.title);
  let conjunction = $state<FilterSet['conjunction']>(init.filterSet.conjunction);
  let rules = $state<FilterRule[]>(init.filterSet.rules.map((r) => ({ ...r })));
  let sortMode = $state<SortMode>(init.sortConfig.mode);
  let sortField = $state<keyof import('$lib/models/types').Task>(init.sortConfig.field ?? 'createdAt');
  let sortDir = $state<SortDirection>(init.sortConfig.direction ?? 'asc');

  // Sort can only target concrete Task fields.
  const FIELD_OPTIONS: { value: keyof Task; label: string }[] = [
    { value: 'status', label: 'Status' },
    { value: 'priority', label: 'Priority' },
    { value: 'dueDate', label: 'Due Date' },
    { value: 'createdAt', label: 'Created' },
    { value: 'updatedAt', label: 'Updated' },
    { value: 'title', label: 'Title' },
    { value: 'tags', label: 'Tags' }
  ];

  // Filters can additionally target synthetic note-derived fields.
  const FILTER_FIELD_OPTIONS: { value: TaskFilterField; label: string }[] = [
    ...FIELD_OPTIONS,
    { value: 'sourcePageId', label: 'Source Note' },
    { value: 'sourcePageTags', label: 'Source Note Tag' }
  ];

  // Notes (pages) available as source-note filter targets.
  const noteOptions = $derived(
    pagesStore.nodes
      .filter((n) => n.type === 'page')
      .map((n) => ({ id: n.id, title: n.title?.trim() || 'Untitled' }))
      .sort((a, b) => a.title.localeCompare(b.title))
  );

  // Distinct tags across all notes, for the source-note-tag autocomplete.
  const noteTagOptions = $derived(
    Array.from(new Set(pagesStore.nodes.flatMap((n) => n.tags ?? []))).sort()
  );

  const OPERATOR_OPTIONS: { value: import('$lib/models/types').FilterOperator; label: string }[] = [
    { value: 'any', label: 'is any value' },
    { value: 'eq', label: 'equals' },
    { value: 'neq', label: 'not equals' },
    { value: 'in', label: 'is one of' },
    { value: 'not_in', label: 'is not one of' },
    { value: 'contains', label: 'contains' },
    { value: 'before', label: 'before' },
    { value: 'after', label: 'after' },
    { value: 'exists', label: 'exists' },
    { value: 'not_exists', label: 'does not exist' }
  ];

    function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) onclose();
  }

  function addRule() {
    rules = [...rules, { id: nanoid(), field: 'status', operator: 'eq', value: 'todo' }];
  }

  /** Reset a rule's value to a sensible default when its target field changes. */
  function onFieldChange(rule: FilterRule) {
    if (rule.field === 'sourcePageId') {
      rule.value = isMultiValue(rule) ? [] : '';
    } else if (rule.field === 'sourcePageTags') {
      rule.value = isMultiValue(rule) ? [] : '';
    } else {
      rule.value = '';
    }
  }

  function removeRule(id: string) {
    rules = rules.filter((r) => r.id !== id);
  }

  async function save() {
    const filterSet: FilterSet = { conjunction, rules };
    const sortConfig: SortConfig = {
      mode: sortMode,
      field: sortMode === 'field' ? sortField : undefined,
      direction: sortMode === 'field' ? sortDir : undefined
    };
    try {
      if (onupdate) {
        await onupdate(lane.id, { title, filterSet, sortConfig });
      } else {
        await lanesStore.updateLane(lane.id, { title, filterSet, sortConfig });
      }
    } catch {
      notificationsStore.error('Failed to save lane configuration.');
    }
    onclose();
  }

  async function deleteLane() {
    try {
      if (ondelete) {
        await ondelete(lane.id);
      } else {
        await lanesStore.deleteLane(lane.id);
      }
    } catch {
      notificationsStore.error('Failed to delete lane.');
    }
    onclose();
  }

  function getRuleValue(rule: FilterRule): string {
    if (Array.isArray(rule.value)) return (rule.value as string[]).join(', ');
    return String(rule.value ?? '');
  }

  function setRuleValue(rule: FilterRule, raw: string) {
    if (rule.operator === 'in' || rule.operator === 'not_in') {
      rule.value = raw.split(',').map((s) => s.trim()).filter(Boolean);
    } else {
      rule.value = raw;
    }
  }

  /** True when the rule targets a specific source note (needs a note picker). */
  function isNoteField(rule: FilterRule): boolean {
    return rule.field === 'sourcePageId';
  }

  /** True when the operator selects multiple values (renders a multi-select). */
  function isMultiValue(rule: FilterRule): boolean {
    return rule.operator === 'in' || rule.operator === 'not_in';
  }

  function getSelectedNoteIds(rule: FilterRule): string[] {
    if (Array.isArray(rule.value)) return rule.value as string[];
    return rule.value ? [String(rule.value)] : [];
  }

  function setSingleNote(rule: FilterRule, id: string) {
    rule.value = id;
  }

  function setMultiNotes(rule: FilterRule, select: HTMLSelectElement) {
    rule.value = Array.from(select.selectedOptions).map((o) => o.value);
  }

  /** Whether the value control should be hidden entirely for this operator. */
  function hidesValue(rule: FilterRule): boolean {
    return rule.operator === 'exists' || rule.operator === 'not_exists' || rule.operator === 'any';
  }
</script>

<div class="modal-backdrop" onclick={handleBackdropClick} onkeydown={(e) => e.key === 'Escape' && onclose()} role="dialog" aria-modal="true" aria-label="Configure lane" tabindex="-1">
  <div class="modal-panel config-modal">
    <div class="modal-header">
      <h2>Configure Lane</h2>
      <button class="btn-ghost icon-btn" onclick={onclose} aria-label="Close">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="18" y1="6" x2="6" y2="18"/>
          <line x1="6" y1="6" x2="18" y2="18"/>
        </svg>
      </button>
    </div>

    <!-- Title -->
    <div class="config-section">
      <label class="section-label" for="lane-title">Lane name</label>
      <input id="lane-title" bind:value={title} />
    </div>

    <!-- Filters -->
    <div class="config-section">
      <div class="section-header-row">
        <span class="section-label">Filters</span>
        {#if rules.length > 1}
          <div class="conjunction-toggle">
            <button class:active={conjunction === 'and'} onclick={() => conjunction = 'and'}>AND</button>
            <button class:active={conjunction === 'or'} onclick={() => conjunction = 'or'}>OR</button>
          </div>
        {/if}
      </div>

      <div class="rules-list">
        {#each rules as rule (rule.id)}
          <div class="rule-row">
            <select bind:value={rule.field} onchange={() => onFieldChange(rule)} class="rule-select">
              {#each FILTER_FIELD_OPTIONS as opt}
                <option value={opt.value}>{opt.label}</option>
              {/each}
            </select>
            <select bind:value={rule.operator} class="rule-select">
              {#each OPERATOR_OPTIONS as opt}
                <option value={opt.value}>{opt.label}</option>
              {/each}
            </select>
            {#if !hidesValue(rule)}
              {#if isNoteField(rule)}
                {#if isMultiValue(rule)}
                  <select
                    class="rule-value note-multi"
                    multiple
                    onchange={(e) => setMultiNotes(rule, e.currentTarget as HTMLSelectElement)}
                  >
                    {#each noteOptions as note}
                      <option value={note.id} selected={getSelectedNoteIds(rule).includes(note.id)}>{note.title}</option>
                    {/each}
                  </select>
                {:else}
                  <select
                    class="rule-value"
                    value={getSelectedNoteIds(rule)[0] ?? ''}
                    onchange={(e) => setSingleNote(rule, (e.target as HTMLSelectElement).value)}
                  >
                    <option value="" disabled>Select a note…</option>
                    {#each noteOptions as note}
                      <option value={note.id}>{note.title}</option>
                    {/each}
                  </select>
                {/if}
              {:else if rule.field === 'sourcePageTags'}
                <input
                  class="rule-value"
                  list="note-tag-options"
                  value={getRuleValue(rule)}
                  oninput={(e) => setRuleValue(rule, (e.target as HTMLInputElement).value)}
                  placeholder={isMultiValue(rule) ? 'tag1, tag2…' : 'tag…'}
                />
              {:else}
                <input
                  class="rule-value"
                  value={getRuleValue(rule)}
                  oninput={(e) => setRuleValue(rule, (e.target as HTMLInputElement).value)}
                  placeholder="value…"
                />
              {/if}
            {/if}
            <button class="btn-ghost icon-btn remove-rule" onclick={() => removeRule(rule.id)} aria-label="Remove rule">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                <line x1="18" y1="6" x2="6" y2="18"/>
                <line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          </div>
        {/each}
      </div>

      <datalist id="note-tag-options">
        {#each noteTagOptions as tag}
          <option value={tag}></option>
        {/each}
      </datalist>

      <button class="btn-ghost add-rule-btn" onclick={addRule}>+ Add filter</button>
    </div>

    <!-- Sort -->
    <div class="config-section">
      <span class="section-label">Sort</span>
      <div class="sort-row">
        <label>
          <input type="radio" bind:group={sortMode} value="auto" />
          Auto (priority + due date)
        </label>
        <label>
          <input type="radio" bind:group={sortMode} value="field" />
          By field
        </label>
        <label>
          <input type="radio" bind:group={sortMode} value="manual" />
          Manual
        </label>
      </div>

      {#if sortMode === 'field'}
        <div class="field-sort-row">
          <select bind:value={sortField}>
            {#each FIELD_OPTIONS as opt}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
          <select bind:value={sortDir}>
            <option value="asc">Ascending</option>
            <option value="desc">Descending</option>
          </select>
        </div>
      {/if}
    </div>

    <div class="modal-footer">
      <button class="btn-danger" onclick={deleteLane}>Delete lane</button>
      <div class="footer-right">
        <button class="btn-ghost" onclick={onclose}>Cancel</button>
        <button class="btn-primary" onclick={save}>Save</button>
      </div>
    </div>
  </div>
</div>

<style>
  .config-modal { max-width: 520px; }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 20px;
  }
  .modal-header h2 { margin: 0; font-size: var(--font-size-lg); color: var(--text-heading); }

  .icon-btn { padding: 4px; border-radius: var(--radius-sm); color: var(--text-muted); line-height: 0; }
  .icon-btn:hover { color: var(--text-primary); }

  .config-section { margin-bottom: 20px; }

  .section-label {
    display: block;
    font-size: var(--font-size-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
    margin-bottom: 8px;
  }

  .section-header-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
  }
  .section-header-row .section-label { margin-bottom: 0; }

  .conjunction-toggle {
    display: flex;
    gap: 0;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .conjunction-toggle button {
    padding: 2px 10px;
    font-size: var(--font-size-xs);
    font-weight: 600;
    background: none;
    border: none;
    border-radius: 0;
    color: var(--text-muted);
  }
  .conjunction-toggle button.active { background: var(--accent); color: #fff; }

  .rules-list { display: flex; flex-direction: column; gap: 6px; margin-bottom: 8px; }

  .rule-row { display: flex; gap: 6px; align-items: center; }

  .rule-select { flex: 1; min-width: 0; }

  .rule-value { flex: 1.2; min-width: 0; }

  .rule-row:has(.note-multi) { align-items: flex-start; }
  .note-multi { min-height: 68px; padding: 4px; }

  .remove-rule { flex-shrink: 0; color: var(--text-muted); }

  .add-rule-btn {
    font-size: var(--font-size-xs);
    color: var(--accent);
    padding: 4px 6px;
  }
  .add-rule-btn:hover { color: var(--accent-hover); }

  .sort-row {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: var(--font-size-sm);
    color: var(--text-secondary);
  }
  .sort-row label { display: flex; align-items: center; gap: 8px; cursor: pointer; }
  .sort-row input[type="radio"] { accent-color: var(--accent); }

  .field-sort-row { display: flex; gap: 8px; margin-top: 8px; }
  .field-sort-row select { flex: 1; }

  .modal-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-top: 16px;
    border-top: 1px solid var(--border-subtle);
  }

  .footer-right { display: flex; gap: 8px; }
</style>
