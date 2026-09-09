<script lang="ts">
  import { page } from '$app/state';
  import { orgsStore } from '$lib/stores/orgs.svelte';
  import { oauthClientsStore } from '$lib/stores/oauthClients.svelte';
  import type { OAuthClient, OAuthClientWithSecret, OAuthScope } from '$lib/models/types';

  const orgId = $derived(page.params.orgId!);
  const org = $derived(orgsStore.orgs.find((o) => o.id === orgId) ?? null);
  const isOwner = $derived(org?.role === 'owner');

  type ResourceRow = { key: 'page' | 'task' | 'template' | 'org'; label: string };
  const RESOURCE_ROWS: ResourceRow[] = [
    { key: 'page', label: 'Pages' },
    { key: 'task', label: 'Tasks' },
    { key: 'template', label: 'Templates' },
    { key: 'org', label: 'Org info' }
  ];

  function scopeReadable(scope: OAuthScope): string {
    const [resource, perm] = scope.split(':');
    const label = RESOURCE_ROWS.find((r) => r.key === resource)?.label ?? resource;
    return `${perm === 'write' ? 'Edit' : 'View'} ${label.toLowerCase()}`;
  }

  // Create form
  let newClientName = $state('');
  let creating = $state(false);
  let createError = $state('');

  // Selection
  let selectedClientId = $state<string | null>(null);
  const selectedClient = $derived(
    oauthClientsStore.clients.find((c) => c.id === selectedClientId) ?? null
  );

  // Editable fields (kept in sync with selection via effect below)
  let editingName = $state('');
  let editingScopes = $state<Set<OAuthScope>>(new Set());
  let editingRedirectUris = $state<string[]>([]);
  let newRedirectUri = $state('');
  let saving = $state(false);
  let saveError = $state('');

  let revealedSecret = $state<{ clientId: string; secret: string } | null>(null);
  let addOrgId = $state('');
  let orgActionError = $state('');

  const ownedOrgs = $derived(orgsStore.orgs.filter((o) => o.role === 'owner'));

  function syncEditState(client: OAuthClient | null) {
    editingName = client?.name ?? '';
    editingScopes = new Set(client?.scopes ?? []);
    editingRedirectUris = client ? [...client.redirectUris] : [];
    saveError = '';
    newRedirectUri = '';
    addOrgId = '';
    orgActionError = '';
  }

  $effect(() => {
    syncEditState(selectedClient);
  });

  async function loadAll() {
    if (!isOwner) return;
    await oauthClientsStore.load(orgId);
  }

  $effect(() => {
    if (orgId && isOwner) loadAll();
  });

  async function createClient() {
    if (!newClientName.trim()) return;
    creating = true;
    createError = '';
    try {
      const created: OAuthClientWithSecret = await oauthClientsStore.createClient(orgId, {
        name: newClientName.trim(),
        scopes: [],
        redirectUris: []
      });
      newClientName = '';
      selectedClientId = created.id;
      revealedSecret = { clientId: created.id, secret: created.clientSecret };
    } catch (e) {
      createError = e instanceof Error ? e.message : 'Failed to create client';
    } finally {
      creating = false;
    }
  }

  function selectClient(id: string) {
    selectedClientId = id;
    revealedSecret = null;
    oauthClientsStore.loadTokens(orgId, id);
  }

  function toggleScope(resource: ResourceRow['key'], perm: 'read' | 'write') {
    const scope = `${resource}:${perm}` as OAuthScope;
    const readScope = `${resource}:read` as OAuthScope;
    const next = new Set(editingScopes);
    if (next.has(scope)) {
      next.delete(scope);
    } else {
      next.add(scope);
      // Editor (write) implies Viewer (read).
      if (perm === 'write') next.add(readScope);
    }
    editingScopes = next;
  }

  function isChecked(resource: ResourceRow['key'], perm: 'read' | 'write'): boolean {
    return editingScopes.has(`${resource}:${perm}` as OAuthScope);
  }

  function isReadDisabled(resource: ResourceRow['key']): boolean {
    return editingScopes.has(`${resource}:write` as OAuthScope);
  }

  function addRedirectUri() {
    const uri = newRedirectUri.trim();
    if (!uri) return;
    try {
      const parsed = new URL(uri);
      if (parsed.protocol !== 'https:' && parsed.hostname !== 'localhost') {
        saveError = 'Redirect URIs must be https:// (or localhost for dev).';
        return;
      }
    } catch {
      saveError = 'Enter a valid absolute URL.';
      return;
    }
    editingRedirectUris = [...editingRedirectUris, uri];
    newRedirectUri = '';
    saveError = '';
  }

  function removeRedirectUri(uri: string) {
    editingRedirectUris = editingRedirectUris.filter((u) => u !== uri);
  }

  async function saveClient() {
    if (!selectedClientId || !editingName.trim()) return;
    saving = true;
    saveError = '';
    try {
      await oauthClientsStore.updateClient(orgId, selectedClientId, {
        name: editingName.trim(),
        scopes: [...editingScopes],
        redirectUris: editingRedirectUris
      });
    } catch (e) {
      saveError = e instanceof Error ? e.message : 'Failed to save changes';
    } finally {
      saving = false;
    }
  }

  async function rotateSecret() {
    if (!selectedClientId) return;
    if (
      !confirm(
        "Rotate this client's secret? The old secret will stop working for new token requests, " +
          'but tokens already issued keep working until they expire.'
      )
    )
      return;
    const rotated = await oauthClientsStore.rotateSecret(orgId, selectedClientId);
    revealedSecret = { clientId: rotated.id, secret: rotated.clientSecret };
  }

  async function rotateSecretAndRevoke() {
    if (!selectedClientId) return;
    if (
      !confirm(
        'Rotate this secret AND revoke every token already issued to this client? ' +
          "Use this if the secret may have leaked — every agent using this client's old " +
          'credentials will need to re-authenticate.'
      )
    )
      return;
    const rotated = await oauthClientsStore.rotateSecret(orgId, selectedClientId, {
      revokeExisting: true
    });
    revealedSecret = { clientId: rotated.id, secret: rotated.clientSecret };
  }

  async function revokeClient() {
    if (!selectedClientId) return;
    if (!confirm('Revoke this client? All of its tokens will stop working immediately.')) return;
    await oauthClientsStore.revokeClient(orgId, selectedClientId);
  }

  async function addOrgToClient() {
    if (!selectedClientId || !addOrgId) return;
    orgActionError = '';
    try {
      await oauthClientsStore.addOrg(orgId, selectedClientId, addOrgId);
      addOrgId = '';
    } catch (e) {
      orgActionError = e instanceof Error ? e.message : 'Failed to add org';
    }
  }

  async function removeOrgFromClient(otherOrgId: string) {
    if (!selectedClientId) return;
    await oauthClientsStore.removeOrg(orgId, selectedClientId, otherOrgId);
  }

  async function revokeToken(tokenId: string) {
    if (!selectedClientId) return;
    await oauthClientsStore.revokeToken(orgId, selectedClientId, tokenId);
  }

  async function revokeAllTokens() {
    if (!selectedClientId) return;
    if (!confirm('Revoke all active tokens for this client?')) return;
    await oauthClientsStore.revokeAllTokens(orgId, selectedClientId);
  }

  function orgName(id: string): string {
    return orgsStore.orgs.find((o) => o.id === id)?.name ?? id;
  }

  async function copySecret(secret: string) {
    try {
      await navigator.clipboard.writeText(secret);
    } catch {
      /* clipboard unavailable — user can still select the text manually */
    }
  }
</script>

<div class="oauth-clients-page">
  <div class="page-header">
    <a class="back-link" href="/settings/orgs">← Organizations</a>
    <h1>OAuth Clients{org ? ` · ${org.name}` : ''}</h1>
    <p class="subtitle">
      Manage machine credentials that let agents and third-party apps act on behalf of your
      organization's members. A client's secret is as sensitive as the org's data: with it, an
      agent can request a token acting as <em>any</em> member, without that member's separate
      consent. Treat it like a shared admin password — if a secret may have leaked, rotate it
      and revoke its existing tokens rather than rotating alone.
    </p>
  </div>

  {#if !oauthClientsStore.available}
    <p class="empty-state">OAuth clients are only available in API mode.</p>
  {:else if !org}
    <p class="empty-state">Loading organization…</p>
  {:else if !isOwner}
    <p class="empty-state">Only organization owners can manage OAuth clients.</p>
  {:else}
    <div class="layout">
      <!-- Left: client list -->
      <div class="clients-list-panel">
        <div class="create-form">
          <input
            class="input"
            placeholder="New client name…"
            bind:value={newClientName}
            onkeydown={(e) => e.key === 'Enter' && createClient()}
          />
          <button
            class="btn-primary"
            onclick={createClient}
            disabled={creating || !newClientName.trim()}
          >
            {creating ? '…' : 'Create'}
          </button>
        </div>
        {#if createError}<p class="field-error">{createError}</p>{/if}

        {#if !oauthClientsStore.loaded}
          <p class="empty-state">Loading…</p>
        {:else if oauthClientsStore.clients.length === 0}
          <p class="empty-state">No OAuth clients yet.</p>
        {:else}
          <ul class="clients-list">
            {#each oauthClientsStore.clients as client (client.id)}
              <li class="client-item" class:selected={client.id === selectedClientId}>
                <button class="client-name-btn" onclick={() => selectClient(client.id)}>
                  <span class="client-name">{client.name}</span>
                  {#if client.revokedAt}
                    <span class="status-badge revoked">revoked</span>
                  {:else}
                    <span class="status-badge active">active</span>
                  {/if}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      <!-- Right: client detail -->
      <div class="client-detail-panel">
        {#if !selectedClient}
          <p class="empty-state">Select a client to manage it.</p>
        {:else}
          {#if revealedSecret && revealedSecret.clientId === selectedClient.id}
            <div class="secret-reveal">
              <p class="secret-warning">
                Copy this secret now — you won't be able to see it again.
              </p>
              <div class="secret-row">
                <code class="secret-value">{revealedSecret.secret}</code>
                <button class="btn-ghost" onclick={() => copySecret(revealedSecret!.secret)}>
                  Copy
                </button>
              </div>
              <button class="btn-ghost dismiss" onclick={() => (revealedSecret = null)}>
                Dismiss
              </button>
            </div>
          {/if}

          <div class="detail-header">
            <input class="input name-input" bind:value={editingName} placeholder="Client name" />
            {#if selectedClient.revokedAt}
              <span class="status-badge revoked">revoked</span>
            {/if}
          </div>

          <div class="client-id-row">
            <span class="field-label">Client ID</span>
            <code class="client-id-value">{selectedClient.clientId}</code>
            <button class="btn-ghost" onclick={() => copySecret(selectedClient.clientId)} title="Copy">
              Copy
            </button>
          </div>

          <section class="section">
            <h3>Scopes</h3>
            <table class="scope-grid">
              <thead>
                <tr>
                  <th></th>
                  <th>Viewer</th>
                  <th>Editor</th>
                </tr>
              </thead>
              <tbody>
                {#each RESOURCE_ROWS as row (row.key)}
                  <tr>
                    <td>{row.label}</td>
                    <td>
                      <input
                        type="checkbox"
                        checked={isChecked(row.key, 'read')}
                        disabled={isReadDisabled(row.key)}
                        onchange={() => toggleScope(row.key, 'read')}
                      />
                    </td>
                    <td>
                      <input
                        type="checkbox"
                        checked={isChecked(row.key, 'write')}
                        onchange={() => toggleScope(row.key, 'write')}
                      />
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </section>

          <section class="section">
            <h3>Organizations</h3>
            <ul class="org-chip-list">
              {#each selectedClient.orgIds as oid (oid)}
                <li class="org-chip">
                  {orgName(oid)}
                  {#if selectedClient.orgIds.length > 1 || oid !== orgId}
                    <button
                      class="chip-remove"
                      onclick={() => removeOrgFromClient(oid)}
                      title="Remove org"
                    >
                      ×
                    </button>
                  {/if}
                </li>
              {/each}
            </ul>
            <div class="add-org-row">
              <select class="perm-select" bind:value={addOrgId}>
                <option value="">Add org…</option>
                {#each ownedOrgs.filter((o) => !selectedClient.orgIds.includes(o.id)) as o (o.id)}
                  <option value={o.id}>{o.name}</option>
                {/each}
              </select>
              <button class="btn-ghost" onclick={addOrgToClient} disabled={!addOrgId}>Add</button>
            </div>
            {#if orgActionError}<p class="field-error">{orgActionError}</p>{/if}
          </section>

          <section class="section">
            <h3>Redirect URIs</h3>
            <p class="section-hint">Used for the authorization-code (user consent) flow.</p>
            <ul class="redirect-list">
              {#each editingRedirectUris as uri (uri)}
                <li class="redirect-row">
                  <code>{uri}</code>
                  <button
                    class="btn-ghost icon-btn danger"
                    onclick={() => removeRedirectUri(uri)}
                    title="Remove"
                  >
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                    </svg>
                  </button>
                </li>
              {/each}
            </ul>
            <div class="add-redirect-row">
              <input
                class="input"
                placeholder="https://agent.example.com/callback"
                bind:value={newRedirectUri}
                onkeydown={(e) => e.key === 'Enter' && addRedirectUri()}
              />
              <button class="btn-ghost" onclick={addRedirectUri}>Add</button>
            </div>
          </section>

          {#if saveError}<p class="field-error">{saveError}</p>{/if}

          <div class="actions-row">
            <button class="btn-primary" onclick={saveClient} disabled={saving || !editingName.trim()}>
              {saving ? 'Saving…' : 'Save changes'}
            </button>
            {#if !selectedClient.revokedAt}
              <button class="btn-ghost" onclick={rotateSecret}>Rotate secret</button>
              <button class="btn-ghost danger" onclick={rotateSecretAndRevoke}>
                Rotate &amp; revoke all tokens
              </button>
              <button class="btn-ghost danger" onclick={revokeClient}>Revoke client</button>
            {/if}
          </div>

          <section class="section">
            <div class="tokens-header">
              <h3>Active tokens</h3>
              {#if oauthClientsStore.tokens.length > 0}
                <button class="btn-ghost danger" onclick={revokeAllTokens}>Revoke all</button>
              {/if}
            </div>
            {#if !oauthClientsStore.tokensLoaded || oauthClientsStore.tokensLoadedClientId !== selectedClient.id}
              <p class="empty-state">Loading…</p>
            {:else if oauthClientsStore.tokens.length === 0}
              <p class="empty-state">No active tokens.</p>
            {:else}
              <ul class="tokens-list">
                {#each oauthClientsStore.tokens as token (token.id)}
                  <li class="token-row">
                    <div class="token-info">
                      <span class="token-user">{token.actingUserEmail ?? token.actingUserId}</span>
                      <span class="token-scopes">{token.scopes.map(scopeReadable).join(', ')}</span>
                    </div>
                    <button
                      class="btn-ghost icon-btn danger"
                      onclick={() => revokeToken(token.id)}
                      title="Revoke"
                    >
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                      </svg>
                    </button>
                  </li>
                {/each}
              </ul>
            {/if}
          </section>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .oauth-clients-page {
    padding: 32px 40px;
    max-width: 960px;
    margin: 0 auto;
  }

  .page-header {
    margin-bottom: 24px;
  }

  .back-link {
    display: inline-block;
    font-size: var(--font-size-sm);
    color: var(--text-muted);
    text-decoration: none;
    margin-bottom: 8px;
  }
  .back-link:hover { color: var(--text-primary); }

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
    grid-template-columns: 260px 1fr;
    gap: 24px;
    align-items: start;
  }

  .clients-list-panel,
  .client-detail-panel {
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-lg);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .client-detail-panel {
    padding: 20px;
    gap: 16px;
  }

  .create-form {
    display: flex;
    gap: 6px;
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

  .btn-ghost {
    background: none;
    border: 1px solid var(--border-default);
    cursor: pointer;
    color: var(--text-muted);
    padding: 6px 10px;
    border-radius: var(--radius-md);
    font-size: var(--font-size-sm);
  }
  .btn-ghost:hover { color: var(--text-primary); }
  .btn-ghost.danger:hover { color: var(--text-danger, #f87171); border-color: var(--text-danger, #f87171); }
  .btn-ghost:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-ghost.icon-btn { padding: 4px; border: none; line-height: 0; }

  .clients-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .client-item {
    border-radius: var(--radius-md);
    padding: 2px;
  }
  .client-item.selected { background: var(--bg-hover); }

  .client-name-btn {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    background: none;
    border: none;
    cursor: pointer;
    text-align: left;
    padding: 6px 8px;
    border-radius: var(--radius-sm);
  }
  .client-name-btn:hover { background: var(--bg-hover); }

  .client-name {
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .status-badge {
    font-size: var(--font-size-xs);
    padding: 2px 6px;
    border-radius: 9999px;
    font-weight: 500;
    flex-shrink: 0;
  }
  .status-badge.active { background: var(--accent-muted); color: var(--accent); }
  .status-badge.revoked { background: rgba(248, 113, 113, 0.15); color: var(--text-danger, #f87171); }

  .detail-header {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .name-input { font-weight: 600; }

  .client-id-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .field-label {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }

  .client-id-value,
  .secret-value,
  .redirect-row code {
    font-family: var(--font-mono, monospace);
    font-size: var(--font-size-xs);
    background: var(--bg-tertiary);
    padding: 3px 6px;
    border-radius: var(--radius-sm);
    word-break: break-all;
  }

  .section h3 {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--text-heading);
    margin: 0 0 8px;
  }

  .section-hint {
    color: var(--text-muted);
    font-size: var(--font-size-xs);
    margin: -4px 0 8px;
  }

  .scope-grid {
    border-collapse: collapse;
    font-size: var(--font-size-sm);
  }
  .scope-grid th, .scope-grid td {
    padding: 4px 12px 4px 0;
    text-align: left;
    color: var(--text-primary);
  }
  .scope-grid th { color: var(--text-muted); font-weight: 500; font-size: var(--font-size-xs); }

  .org-chip-list {
    list-style: none;
    margin: 0 0 8px;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .org-chip {
    display: flex;
    align-items: center;
    gap: 4px;
    background: var(--bg-tertiary);
    color: var(--text-primary);
    font-size: var(--font-size-xs);
    padding: 3px 6px 3px 10px;
    border-radius: 9999px;
  }

  .chip-remove {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-muted);
    font-size: 1em;
    line-height: 1;
    padding: 0 2px;
  }
  .chip-remove:hover { color: var(--text-danger, #f87171); }

  .add-org-row, .add-redirect-row {
    display: flex;
    gap: 8px;
  }

  .perm-select {
    padding: 7px 8px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }

  .redirect-list {
    list-style: none;
    margin: 0 0 8px;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .redirect-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .actions-row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .tokens-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .tokens-header h3 { margin: 0; }

  .tokens-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .token-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 0;
    border-bottom: 1px solid var(--border-subtle);
  }
  .token-row:last-child { border-bottom: none; }

  .token-info {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .token-user {
    font-size: var(--font-size-sm);
    color: var(--text-primary);
  }
  .token-scopes {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }

  .secret-reveal {
    background: var(--accent-muted);
    border: 1px solid var(--accent);
    border-radius: var(--radius-md);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .secret-warning {
    margin: 0;
    font-size: var(--font-size-sm);
    font-weight: 500;
    color: var(--text-primary);
  }
  .secret-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .secret-value { flex: 1; }
  .dismiss { align-self: flex-start; }

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

  @media (max-width: 768px) {
    .oauth-clients-page { padding: 56px 40px 32px; }
  }

  @media (max-width: 640px) {
    .oauth-clients-page { padding: 56px 16px 16px; }
    .layout { grid-template-columns: 1fr; }
  }
</style>
