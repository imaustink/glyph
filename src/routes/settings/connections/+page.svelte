<script lang="ts">
  import { browser } from '$app/environment';
  import { format, formatDistanceToNow, parseISO } from 'date-fns';
  import { repositories } from '$lib/storage/config';
  import { handleAuthError } from '$lib/storage/apiClient';
  import { scopeReadable } from '$lib/utils/oauthScopes';
  import type { OAuthConnection } from '$lib/models/types';

  const repo = repositories.oauthConnections;

  let connections = $state<OAuthConnection[]>([]);
  let loading = $state(true);
  let error = $state('');
  let revokingId = $state<string | null>(null);

  async function load() {
    if (!browser || !repo) return;
    loading = true;
    error = '';
    try {
      connections = await repo.list();
    } catch (e) {
      try {
        handleAuthError(e);
      } catch {
        error = e instanceof Error ? e.message : 'Failed to load connected apps';
      }
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load();
  });

  async function revoke(conn: OAuthConnection) {
    if (!repo) return;
    if (
      !confirm(
        `Revoke access for ${conn.clientName}? It will immediately lose access to your account and will need to be reconnected.`
      )
    )
      return;
    revokingId = conn.id;
    error = '';
    try {
      await repo.revoke(conn.id);
      connections = connections.filter((c) => c.id !== conn.id);
    } catch (e) {
      try {
        handleAuthError(e);
      } catch {
        error = e instanceof Error ? e.message : 'Failed to revoke access';
      }
    } finally {
      revokingId = null;
    }
  }

  function workspaceNames(conn: OAuthConnection): string[] {
    // `?? []`: a nil Go slice serializes as null.
    return [
      ...(conn.personal ? ['Personal workspace'] : []),
      ...(conn.orgs ?? []).map((o) => o.name)
    ];
  }

  function formatDate(iso: string): string {
    return format(parseISO(iso), 'MMM d, yyyy');
  }

  function formatRelative(iso: string): string {
    return formatDistanceToNow(parseISO(iso), { addSuffix: true });
  }
</script>

<div class="connections-page">
  <div class="page-header">
    <h1>Connected apps</h1>
    <p class="subtitle">Apps and AI agents you've allowed to access your account.</p>
  </div>

  {#if !repo}
    <p class="empty-state">Connected apps are only available in API mode.</p>
  {:else if loading}
    <p class="empty-state">Loading…</p>
  {:else}
    {#if error}<p class="field-error">{error}</p>{/if}
    {#if connections.length === 0}
      <p class="empty-state">No apps are connected to your account.</p>
    {:else}
      <ul class="connection-list">
        {#each connections as conn (conn.id)}
          <li class="connection-card">
            <div class="connection-header">
              <div class="connection-title">
                <span class="connection-name">{conn.clientName}</span>
                {#if conn.dynamic}
                  <span
                    class="badge badge-unverified"
                    title="This app registered itself and hasn't been verified by Glyph."
                  >
                    Unverified
                  </span>
                {/if}
              </div>
              <button
                class="btn-danger"
                onclick={() => revoke(conn)}
                disabled={revokingId === conn.id}
              >
                {revokingId === conn.id ? '…' : 'Revoke'}
              </button>
            </div>

            <dl class="connection-details">
              <dt>Workspaces</dt>
              <dd>
                {#if workspaceNames(conn).length === 0}
                  <span class="muted">None</span>
                {:else}
                  <span class="chip-row">
                    {#each workspaceNames(conn) as name (name)}
                      <span class="chip">{name}</span>
                    {/each}
                  </span>
                {/if}
              </dd>

              <dt>Access</dt>
              <dd>{(conn.scopes ?? []).map(scopeReadable).join(', ') || 'None'}</dd>

              <dt>Connected</dt>
              <dd>{formatDate(conn.createdAt)}</dd>

              <dt>Last used</dt>
              <dd>
                {#if conn.lastUsedAt}
                  <span title={formatDate(conn.lastUsedAt)}>{formatRelative(conn.lastUsedAt)}</span>
                {:else}
                  <span class="muted">Never</span>
                {/if}
              </dd>
            </dl>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</div>

<style>
  .connections-page {
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

  .connection-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .connection-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-lg);
    padding: 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .connection-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .connection-title {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .connection-name {
    font-size: var(--font-size-md);
    font-weight: 600;
    color: var(--text-heading);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .badge {
    font-size: var(--font-size-xs);
    padding: 2px 6px;
    border-radius: 9999px;
    font-weight: 500;
    white-space: nowrap;
  }
  .badge-unverified { background: rgba(234, 179, 8, 0.15); color: #ca8a04; }

  .connection-details {
    display: grid;
    grid-template-columns: 100px 1fr;
    gap: 6px 12px;
    margin: 0;
    font-size: var(--font-size-sm);
  }

  .connection-details dt {
    color: var(--text-muted);
  }

  .connection-details dd {
    margin: 0;
    color: var(--text-primary);
    min-width: 0;
  }

  .chip-row {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .chip {
    font-size: var(--font-size-xs);
    padding: 2px 8px;
    border-radius: 9999px;
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }

  .muted { color: var(--text-muted); }

  .btn-danger {
    padding: 6px 12px;
    border-radius: var(--radius-md);
    background: none;
    border: 1px solid var(--border-default);
    color: var(--text-danger, #f87171);
    font-size: var(--font-size-sm);
    cursor: pointer;
    white-space: nowrap;
  }
  .btn-danger:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-danger:not(:disabled):hover { background: var(--bg-hover); }

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
    margin: 0 0 12px;
  }

  /* Clear the fixed hamburger button (top:10 + height:36 = 46px), shown at <=768px */
  @media (max-width: 768px) {
    .connections-page { padding: 56px 40px 32px; }
  }

  @media (max-width: 640px) {
    .connections-page { padding: 56px 16px 16px; }
    .connection-details { grid-template-columns: 1fr; gap: 2px; }
    .connection-details dd { margin-bottom: 8px; }
  }
</style>
