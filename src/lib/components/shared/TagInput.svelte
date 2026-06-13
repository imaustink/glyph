<script lang="ts">
  let {
    tags = $bindable<string[]>([]),
    suggestions = [],
    onchange
  }: {
    tags: string[];
    suggestions?: string[];
    onchange?: (tags: string[]) => void;
  } = $props();

  let inputVal = $state('');
  let showSuggestions = $state(false);

  const filtered = $derived(
    suggestions.filter((s) => s.toLowerCase().startsWith(inputVal.toLowerCase()) && !tags.includes(s))
  );

  function addTag(tag: string) {
    const trimmed = tag.trim();
    if (trimmed && !tags.includes(trimmed)) {
      tags = [...tags, trimmed];
      onchange?.(tags);
    }
    inputVal = '';
    showSuggestions = false;
  }

  function removeTag(tag: string) {
    tags = tags.filter((t) => t !== tag);
    onchange?.(tags);
  }

  function handleKeydown(e: KeyboardEvent) {
    if ((e.key === 'Enter' || e.key === ',') && inputVal.trim()) {
      e.preventDefault();
      addTag(inputVal);
    }
    if (e.key === 'Backspace' && !inputVal && tags.length) {
      tags = tags.slice(0, -1);
      onchange?.(tags);
    }
    if (e.key === 'Escape') showSuggestions = false;
  }
</script>

<div class="tag-input-wrapper">
  <div class="tags-container">
    {#each tags as tag}
      <span class="tag-pill">
        {tag}
        <button class="remove-tag" onclick={() => removeTag(tag)} type="button" aria-label="Remove tag">×</button>
      </span>
    {/each}
    <input
      class="tag-text-input"
      bind:value={inputVal}
      onkeydown={handleKeydown}
      onfocus={() => showSuggestions = true}
      onblur={() => setTimeout(() => showSuggestions = false, 150)}
      placeholder={tags.length === 0 ? 'Add tags…' : ''}
    />
  </div>

  {#if showSuggestions && filtered.length > 0}
    <div class="suggestions">
      {#each filtered.slice(0, 8) as s}
        <button class="suggestion-item" onmousedown={() => addTag(s)} type="button">{s}</button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .tag-input-wrapper { position: relative; }

  .tags-container {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    align-items: center;
    background: var(--bg-tertiary);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    padding: 5px 8px;
    min-height: 34px;
    cursor: text;
  }
  .tags-container:focus-within { border-color: var(--accent); }

  .tag-pill { display: inline-flex; align-items: center; gap: 4px; }

  .remove-tag {
    background: none;
    border: none;
    padding: 0;
    color: var(--tag-text);
    font-size: 14px;
    line-height: 1;
    cursor: pointer;
    opacity: 0.7;
  }
  .remove-tag:hover { opacity: 1; }

  .tag-text-input {
    flex: 1;
    background: none;
    border: none;
    padding: 0;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    min-width: 60px;
    outline: none;
  }

  .suggestions {
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
  }

  .suggestion-item {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    padding: 6px 10px;
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    cursor: pointer;
  }
  .suggestion-item:hover { background: var(--bg-hover); }
</style>
