<script lang="ts">
  import { orgsStore } from '$lib/stores/orgs.svelte';

  interface Props {
    orgId: string | null | undefined;
    isPrivate: boolean | undefined;
    onchange: (orgId: string | null, isPrivate: boolean) => void;
    onshare?: () => void;
  }

  let { orgId, isPrivate, onchange, onshare }: Props = $props();

  let open = $state(false);
  let containerRef = $state<HTMLDivElement | null>(null);
  let buttonRef = $state<HTMLButtonElement | null>(null);
  let dropdownRef = $state<HTMLDivElement | null>(null);
  let dropdownStyle = $state('');

  const currentOrg = $derived(
    orgId ? orgsStore.orgs.find((o) => o.id === orgId) ?? null : null
  );

  const label = $derived(
    isPrivate || !orgId
      ? 'Private'
      : (currentOrg?.name ?? 'Org')
  );

  function select(newOrgId: string | null, newIsPrivate: boolean) {
    open = false;
    onchange(newOrgId, newIsPrivate);
  }

  function handleShareClick() {
    open = false;
    onshare?.();
  }

  function handleClickOutside(event: MouseEvent) {
    // Check both container and dropdown (dropdown is now outside container due to fixed positioning)
    const target = event.target as Node;
    const clickedInsideContainer = containerRef?.contains(target);
    const clickedInsideDropdown = dropdownRef?.contains(target);
    if (!clickedInsideContainer && !clickedInsideDropdown) {
      open = false;
    }
  }

  function updateDropdownPosition() {
    if (buttonRef) {
      const rect = buttonRef.getBoundingClientRect();
      const dropdownWidth = 220;
      // Position dropdown below button
      // Prefer aligning left edge, but if that would go off-screen right, align right edge instead
      let left = rect.left;
      if (left + dropdownWidth > window.innerWidth) {
        left = rect.right - dropdownWidth;
      }
      // Ensure it doesn't go off the left edge
      if (left < 0) {
        left = 0;
      }
      dropdownStyle = `top: ${rect.bottom + 6}px; left: ${left}px;`;
    }
  }

  $effect(() => {
    if (open) {
      updateDropdownPosition();
      // Use setTimeout to avoid immediately closing from the opening click
      let timeoutId = setTimeout(() => {
        document.addEventListener('click', handleClickOutside);
      }, 0);
      window.addEventListener('scroll', updateDropdownPosition, true);
      window.addEventListener('resize', updateDropdownPosition);
      return () => {
        clearTimeout(timeoutId);
        document.removeEventListener('click', handleClickOutside);
        window.removeEventListener('scroll', updateDropdownPosition, true);
        window.removeEventListener('resize', updateDropdownPosition);
      };
    }
  });
</script>

<div class="visibility-picker" bind:this={containerRef}>
  <button
    class="visibility-btn"
    class:private={isPrivate || !orgId}
    onclick={() => open = !open}
    aria-haspopup="listbox"
    aria-expanded={open}
    bind:this={buttonRef}
  >
    {#if isPrivate || !orgId}
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
        <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
      </svg>
    {:else}
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
        <circle cx="9" cy="7" r="4"/>
        <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
        <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
      </svg>
    {/if}
    {label}
    <svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor" class="chevron" class:rotated={open}>
      <path d="M2 3l3 3 3-3" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round"/>
    </svg>
  </button>

  {#if open}
    <div class="dropdown" role="listbox" bind:this={dropdownRef} style={dropdownStyle}>
      <button
        class="option"
        class:selected={isPrivate || !orgId}
        role="option"
        aria-selected={isPrivate || !orgId}
        onclick={() => select(null, true)}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
          <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
        </svg>
        <div class="option-text">
          <span class="option-label">Private</span>
          <span class="option-desc">Only you can see this</span>
        </div>
        {#if isPrivate || !orgId}
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="check">
            <polyline points="20 6 9 17 4 12"/>
          </svg>
        {/if}
      </button>

      {#if orgsStore.orgs.length > 0}
        <div class="option-divider"></div>
        <p class="option-group-label">Share with organization</p>
        {#each orgsStore.orgs as org (org.id)}
          <button
            class="option"
            class:selected={orgId === org.id && !isPrivate}
            role="option"
            aria-selected={orgId === org.id && !isPrivate}
            onclick={() => select(org.id, false)}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
              <circle cx="9" cy="7" r="4"/>
              <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
              <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
            </svg>
            <div class="option-text">
              <span class="option-label">{org.name}</span>
              <span class="option-desc">All members can view</span>
            </div>
            {#if orgId === org.id && !isPrivate}
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="check">
                <polyline points="20 6 9 17 4 12"/>
              </svg>
            {/if}
          </button>
        {/each}
      {/if}

      <div class="option-divider"></div>
      <button class="option share-option" onclick={handleShareClick}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/>
          <line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/>
          <line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/>
        </svg>
        <div class="option-text">
          <span class="option-label">Share with people…</span>
          <span class="option-desc">Invite specific people by email</span>
        </div>
      </button>
    </div>
  {/if}
</div>

<style>
  .visibility-picker {
    position: relative;
    display: inline-flex;
    margin-left: auto;
  }

  .visibility-btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 4px 8px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border-default);
    background: var(--bg-secondary);
    color: var(--text-secondary);
    font-size: var(--font-size-xs);
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast);
    white-space: nowrap;
  }

  .visibility-btn:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }

  .chevron { transition: transform var(--transition-fast); }
  .chevron.rotated { transform: rotate(180deg); }

  .dropdown {
    position: fixed;
    z-index: 1100;
    min-width: 220px;
    background: var(--bg-modal);
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.25);
    padding: 4px;
  }

  .option {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px 10px;
    border: none;
    background: none;
    border-radius: var(--radius-sm);
    cursor: pointer;
    text-align: left;
    color: var(--text-primary);
  }

  .option:hover { background: var(--bg-hover); }
  .option.selected { background: var(--accent-bg); }

  .option-text {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .option-label {
    font-size: var(--font-size-sm);
    font-weight: 500;
    color: var(--text-primary);
  }

  .option-desc {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }

  .option-divider {
    height: 1px;
    background: var(--border-subtle);
    margin: 4px 0;
  }

  .option-group-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 4px 10px 2px;
    margin: 0;
  }

  .check { color: var(--accent); flex-shrink: 0; }
</style>
