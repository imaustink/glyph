<script lang="ts">
  import { orgsStore } from '$lib/stores/orgs.svelte';
  import type { OrgMember, OrgRole, Organization } from '$lib/models/types';

  let newOrgName = $state('');
  let creating = $state(false);
  let createError = $state('');

  // Expanded org detail view
  let selectedOrgId = $state<string | null>(null);
  let members = $state<OrgMember[]>([]);
  let membersLoading = $state(false);
  let membersError = $state('');

  // Add member
  let memberEmail = $state('');
  let newMemberRole = $state<OrgRole>('viewer');
  let addingMember = $state(false);
  let addMemberError = $state('');

  // Edit org name
  let editingOrgId = $state<string | null>(null);
  let editingName = $state('');

  const selectedOrg = $derived(orgsStore.orgs.find((o) => o.id === selectedOrgId) ?? null);

  async function createOrg() {
    if (!newOrgName.trim()) return;
    creating = true;
    createError = '';
    try {
      const org = await orgsStore.createOrg(newOrgName.trim());
      newOrgName = '';
      await selectOrg(org.id);
    } catch (e) {
      createError = e instanceof Error ? e.message : 'Failed to create org';
    } finally {
      creating = false;
    }
  }

  async function selectOrg(id: string) {
    selectedOrgId = id;
    membersLoading = true;
    membersError = '';
    members = [];
    try {
      const detail = await orgsStore.getOrgDetail(id);
      members = detail?.members ?? [];
    } catch (e) {
      membersError = e instanceof Error ? e.message : 'Failed to load members';
    } finally {
      membersLoading = false;
    }
  }

  async function deleteOrg(id: string) {
    if (!confirm('Delete this organization? This cannot be undone.')) return;
    await orgsStore.deleteOrg(id);
    if (selectedOrgId === id) selectedOrgId = null;
  }

  async function startEditOrg(org: Organization & { role: string }) {
    editingOrgId = org.id;
    editingName = org.name;
  }

  async function commitEditOrg() {
    if (!editingOrgId || !editingName.trim()) return;
    await orgsStore.updateOrg(editingOrgId, editingName.trim());
    editingOrgId = null;
  }

  const memberEmailValid = $derived(/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(memberEmail.trim()));

  async function addMember() {
    if (!selectedOrgId || !memberEmailValid) return;
    addingMember = true;
    addMemberError = '';
    try {
      const member = await orgsStore.addMember(selectedOrgId, memberEmail.trim(), newMemberRole);
      members = [...members, member];
      memberEmail = '';
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to add member';
      addMemberError = msg.includes('no user found') ? 'No account found with that email.' : msg;
    } finally {
      addingMember = false;
    }
  }

  async function updateRole(userId: string, role: OrgRole) {
    if (!selectedOrgId) return;
    try {
      const updated = await orgsStore.updateMemberRole(selectedOrgId, userId, role);
      members = members.map((m) => (m.userId === userId ? updated : m));
    } catch (e) {
      membersError = e instanceof Error ? e.message : 'Failed to update role';
    }
  }

  async function removeMember(userId: string) {
    if (!selectedOrgId) return;
    await orgsStore.removeMember(selectedOrgId, userId);
    members = members.filter((m) => m.userId !== userId);
  }

  function displayName(m: OrgMember) {
    return m.name ?? m.email ?? m.userId;
  }
</script>

<div class="orgs-page">
  <div class="page-header">
    <h1>Organizations</h1>
    <p class="subtitle">Manage your organizations and invite collaborators.</p>
  </div>

  {#if !orgsStore.available}
    <p class="empty-state">Organizations are only available in API mode.</p>
  {:else}
    <div class="layout">
      <!-- Left: org list -->
      <div class="orgs-list-panel">
        <div class="create-form">
          <input
            class="input"
            placeholder="New organization name…"
            bind:value={newOrgName}
            onkeydown={(e) => e.key === 'Enter' && createOrg()}
          />
          <button class="btn-primary" onclick={createOrg} disabled={creating || !newOrgName.trim()}>
            {creating ? '…' : 'Create'}
          </button>
        </div>
        {#if createError}<p class="field-error">{createError}</p>{/if}

        {#if orgsStore.orgs.length === 0}
          <p class="empty-state">No organizations yet.</p>
        {:else}
          <ul class="orgs-list">
            {#each orgsStore.orgs as org (org.id)}
              <li class="org-item" class:selected={org.id === selectedOrgId}>
                {#if editingOrgId === org.id}
                  <input
                    class="input inline-input"
                    bind:value={editingName}
                    onblur={commitEditOrg}
                    onkeydown={(e) => { if (e.key === 'Enter') commitEditOrg(); if (e.key === 'Escape') editingOrgId = null; }}
                  />
                {:else}
                  <button class="org-name-btn" onclick={() => selectOrg(org.id)}>
                    <span class="org-name">{org.name}</span>
                    <span class="org-role role-{org.role}">{org.role}</span>
                  </button>
                {/if}
                <div class="org-actions">
                  {#if org.role === 'owner' && editingOrgId !== org.id}
                    <button class="btn-ghost icon-btn" onclick={() => startEditOrg(org)} title="Rename">
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
                        <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
                      </svg>
                    </button>
                    <button class="btn-ghost icon-btn danger" onclick={() => deleteOrg(org.id)} title="Delete">
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/>
                        <path d="M10 11v6"/><path d="M14 11v6"/>
                      </svg>
                    </button>
                  {/if}
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      <!-- Right: members panel -->
      <div class="members-panel">
        {#if !selectedOrg}
          <p class="empty-state">Select an organization to manage members.</p>
        {:else}
          <h2>{selectedOrg.name}</h2>

          {#if selectedOrg.role !== 'viewer'}
            <div class="add-member-row">
              <input
                class="input"
                type="email"
                placeholder="Email address…"
                bind:value={memberEmail}
                onkeydown={(e) => e.key === 'Enter' && memberEmailValid && !addingMember && addMember()}
                autocomplete="off"
              />
              <select class="perm-select" bind:value={newMemberRole}>
                <option value="viewer">Viewer</option>
                <option value="editor">Editor</option>
                <option value="owner">Owner</option>
              </select>
              <button class="btn-primary" onclick={addMember} disabled={!memberEmailValid || addingMember}>
                {addingMember ? '…' : 'Add'}
              </button>
            </div>
            {#if addMemberError}<p class="field-error">{addMemberError}</p>{/if}
          {/if}

          {#if membersLoading}
            <p class="empty-state">Loading…</p>
          {:else if membersError}
            <p class="field-error">{membersError}</p>
          {:else if members.length === 0}
            <p class="empty-state">No members yet.</p>
          {:else}
            <ul class="members-list">
              {#each members as member (member.userId)}
                <li class="member-row">
                  <span class="user-avatar">{displayName(member)[0].toUpperCase()}</span>
                  <div class="user-info">
                    <span class="user-name">{displayName(member)}</span>
                    {#if member.email && member.name}
                      <span class="user-email">{member.email}</span>
                    {/if}
                  </div>
                  {#if selectedOrg.role === 'owner'}
                    <select
                      class="perm-select"
                      value={member.role}
                      onchange={(e) => updateRole(member.userId, (e.target as HTMLSelectElement).value as OrgRole)}
                    >
                      <option value="viewer">Viewer</option>
                      <option value="editor">Editor</option>
                      <option value="owner">Owner</option>
                    </select>
                    <button class="btn-ghost icon-btn danger" onclick={() => removeMember(member.userId)} title="Remove">
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                      </svg>
                    </button>
                  {:else}
                    <span class="org-role role-{member.role}">{member.role}</span>
                  {/if}
                </li>
              {/each}
            </ul>
          {/if}
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .orgs-page {
    padding: 32px 40px;
    max-width: 860px;
    margin: 0 auto;
  }

  .page-header {
    margin-bottom: 24px;
  }

  .page-header h1 {
    font-size: 1.6em;
    font-weight: 700;
    color: var(--text-heading);
    margin: 0 0 4px;
  }

  .subtitle {
    color: var(--text-muted);
    font-size: var(--font-size-sm);
    margin: 0;
  }

  .layout {
    display: grid;
    grid-template-columns: 240px 1fr;
    gap: 24px;
    align-items: start;
  }

  .orgs-list-panel {
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-lg);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .create-form {
    display: flex;
    gap: 6px;
  }

  .create-form .btn-primary {
    padding: 7px 10px;
    flex-shrink: 0;
  }

  .input {
    flex: 1;
    padding: 7px 10px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
  }
  .input:focus { outline: none; border-color: var(--accent); }

  .inline-input { flex: 1; }

  .btn-primary {
    padding: 7px 14px;
    border-radius: var(--radius-md);
    background: var(--accent);
    color: white;
    font-size: var(--font-size-sm);
    font-weight: 500;
    border: none;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-primary:not(:disabled):hover { opacity: 0.9; }

  .orgs-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .org-item {
    display: flex;
    align-items: center;
    gap: 4px;
    border-radius: var(--radius-md);
    padding: 4px;
  }
  .org-item.selected { background: var(--bg-hover); }

  .org-name-btn {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 8px;
    background: none;
    border: none;
    cursor: pointer;
    text-align: left;
    padding: 4px 6px;
    border-radius: var(--radius-sm);
  }
  .org-name-btn:hover { background: var(--bg-hover); }

  .org-name {
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    font-weight: 500;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .org-actions { display: flex; gap: 2px; }

  .members-panel {
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-lg);
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .members-panel h2 {
    font-size: var(--font-size-md);
    font-weight: 600;
    color: var(--text-heading);
    margin: 0;
  }

  .add-member-row {
    display: flex;
    gap: 8px;
    align-items: flex-start;
  }

  .add-member-row .input { flex: 1; }

  .perm-select {
    padding: 7px 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }

  .members-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .member-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 0;
    border-bottom: 1px solid var(--border-subtle);
  }
  .member-row:last-child { border-bottom: none; }

  .user-avatar {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    background: var(--accent-muted);
    color: var(--accent);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: var(--font-size-sm);
    font-weight: 600;
    flex-shrink: 0;
  }

  .user-info {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .user-name {
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .user-email {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .org-role {
    font-size: var(--font-size-xs);
    padding: 2px 6px;
    border-radius: 9999px;
    font-weight: 500;
    text-transform: capitalize;
  }
  .role-owner { background: var(--accent-muted); color: var(--accent); }
  .role-editor { background: rgba(234, 179, 8, 0.15); color: #ca8a04; }
  .role-viewer { background: var(--bg-tertiary); color: var(--text-muted); }

  .empty-state {
    color: var(--text-muted);
    font-size: var(--font-size-sm);
    text-align: center;
    padding: 24px 0;
    margin: 0;
  }

  .field-error {
    color: var(--text-danger, #f87171);
    font-size: var(--font-size-xs);
    margin: 0;
  }

  .btn-ghost {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-muted);
    padding: 4px;
    border-radius: var(--radius-sm);
    line-height: 0;
  }
  .btn-ghost:hover { color: var(--text-primary); }
  .btn-ghost.danger:hover { color: var(--text-danger, #f87171); }

  .icon-btn { line-height: 0; }

  /* Clear the fixed hamburger button (top:10 + height:36 = 46px), shown at <=768px */
  @media (max-width: 768px) {
    .orgs-page { padding: 56px 40px 32px; }
  }

  @media (max-width: 640px) {
    .orgs-page { padding: 56px 16px 16px; }
    .layout { grid-template-columns: 1fr; }
  }
</style>
