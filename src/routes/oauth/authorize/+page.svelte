<script lang="ts">
  import { page } from '$app/state';
  import { browser } from '$app/environment';
  import { oauthConsentRepository } from '$lib/storage/repositories/ApiOAuthConsentRepository';
  import { ApiError, UnauthorizedError, API_BASE } from '$lib/storage/apiClient';
  import { scopeReadable } from '$lib/utils/oauthScopes';
  import type { OAuthConsentInfo } from '$lib/models/types';

  let info = $state<OAuthConsentInfo | null>(null);
  let loading = $state(true);
  let error = $state('');
  let deciding = $state(false);
  // Error from a failed decision; unlike `error`, it keeps the form visible so
  // the user can adjust their selection and retry.
  let decideError = $state('');

  // Selection mode only: which workspaces the user is granting.
  let personalSelected = $state(true);
  let selectedOrgIds = $state<Set<string>>(new Set());

  // `?? []`: a nil Go slice serializes as null.
  const workspaces = $derived(info?.workspaces ?? []);
  const personalWorkspace = $derived(workspaces.find((w) => w.kind === 'personal') ?? null);
  const orgWorkspaces = $derived(workspaces.filter((w) => w.kind === 'org'));
  const hasSelection = $derived(
    (personalWorkspace !== null && personalSelected) || selectedOrgIds.size > 0
  );
  const canApprove = $derived(!!info && (!info.workspaceSelection || hasSelection));

  function toggleOrg(id: string) {
    const next = new Set(selectedOrgIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selectedOrgIds = next;
  }

  /** Prefer the API's OAuth-style error_description over the generic status message. */
  function errorMessage(e: unknown, fallback: string): string {
    if (e instanceof ApiError && e.body && typeof e.body === 'object') {
      const desc = (e.body as { error_description?: unknown }).error_description;
      if (typeof desc === 'string' && desc) return desc;
    }
    return e instanceof Error ? e.message : fallback;
  }

  async function load() {
    if (!browser) return;
    loading = true;
    error = '';
    try {
      info = await oauthConsentRepository.getInfo(page.url.searchParams.toString());
      // Default selection: personal checked, orgs unchecked.
      personalSelected = true;
      selectedOrgIds = new Set();
    } catch (e) {
      if (e instanceof UnauthorizedError) {
        const next = encodeURIComponent(location.pathname + location.search);
        location.assign(`${API_BASE}/auth/login?next=${next}`);
        return;
      }
      error = errorMessage(e, 'This authorization request is invalid.');
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load();
  });

  async function decide(approve: boolean) {
    if (!info) return;
    if (approve && !canApprove) return;
    deciding = true;
    decideError = '';
    try {
      const selection = info.workspaceSelection
        ? {
            personal: personalWorkspace !== null && personalSelected,
            orgIds: orgWorkspaces.filter((w) => selectedOrgIds.has(w.id)).map((w) => w.id)
          }
        : undefined;
      const result = await oauthConsentRepository.decide(info.consentToken, approve, selection);
      window.location.href = result.redirectUrl;
    } catch (e) {
      decideError = errorMessage(e, 'Failed to record your decision.');
      deciding = false;
    }
  }
</script>

<div class="consent-page">
  <div class="consent-card">
    {#if loading}
      <p class="consent-status">Loading…</p>
    {:else if error}
      <h1>Can't complete this request</h1>
      <p class="consent-error">{error}</p>
    {:else if info}
      <h1>{info.client.name} wants to access your account</h1>
      {#if !info.workspaceSelection}
        <p class="consent-org">for organization <strong>{info.orgName}</strong></p>
      {/if}

      {#if info.client.dynamic}
        <p class="consent-warning" role="note">
          This app registered itself and hasn't been verified by Glyph. Only continue if you
          started this connection.
        </p>
        {#if info.client.redirectHost}
          <p class="consent-redirect">
            It will redirect to <strong>{info.client.redirectHost}</strong>.
          </p>
        {/if}
      {/if}

      <ul class="scope-list">
        {#each info.scopes as scope (scope)}
          <li>{scopeReadable(scope)}</li>
        {/each}
      </ul>

      {#if info.workspaceSelection}
        <fieldset class="workspace-picker">
          <legend>Choose which workspaces to share</legend>
          {#if personalWorkspace}
            <label class="workspace-option">
              <input type="checkbox" bind:checked={personalSelected} disabled={deciding} />
              <span class="workspace-text">
                <span class="workspace-name">Personal workspace</span>
                <span class="workspace-help">
                  Your own pages and tasks, including ones shared directly with you
                </span>
              </span>
            </label>
          {/if}
          {#each orgWorkspaces as ws (ws.id)}
            <label class="workspace-option">
              <input
                type="checkbox"
                checked={selectedOrgIds.has(ws.id)}
                onchange={() => toggleOrg(ws.id)}
                disabled={deciding}
              />
              <span class="workspace-text">
                <span class="workspace-name">{ws.name}</span>
                <span class="workspace-help">Organization</span>
              </span>
            </label>
          {/each}
        </fieldset>
        {#if !hasSelection}
          <p class="consent-hint">Select at least one workspace to continue.</p>
        {/if}
      {/if}

      <p class="consent-disclaimer">
        {#if info.workspaceSelection}
          This will let {info.client.name} act on your behalf within the workspaces you select.
        {:else}
          This will let {info.client.name} act on your behalf within {info.orgName}.
        {/if}
        You can revoke access at any time from Settings → Connected apps.
      </p>

      {#if decideError}
        <p class="consent-error">{decideError}</p>
      {/if}

      <div class="consent-actions">
        <button class="btn-ghost" onclick={() => decide(false)} disabled={deciding}>Deny</button>
        <button class="btn-primary" onclick={() => decide(true)} disabled={deciding || !canApprove}>
          {deciding ? '…' : 'Allow'}
        </button>
      </div>
    {/if}
  </div>
</div>

<style>
  .consent-page {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
  }

  .consent-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-lg);
    padding: 32px;
    max-width: 420px;
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .consent-card h1 {
    font-size: 1.2em;
    font-weight: 700;
    color: var(--text-heading);
    margin: 0;
  }

  .consent-status {
    color: var(--text-muted);
    font-size: var(--font-size-sm);
    margin: 0;
    text-align: center;
  }

  .consent-error {
    color: var(--text-danger, #f87171);
    font-size: var(--font-size-sm);
    margin: 0;
  }

  .consent-org {
    color: var(--text-muted);
    font-size: var(--font-size-sm);
    margin: 0;
  }

  .scope-list {
    list-style: none;
    margin: 8px 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .scope-list li {
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    padding: 6px 10px;
    background: var(--bg-tertiary);
    border-radius: var(--radius-md);
  }

  .consent-redirect {
    color: var(--text-muted);
    font-size: var(--font-size-sm);
    margin: 0;
    overflow-wrap: anywhere;
  }

  .consent-warning {
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    margin: 0;
    padding: 8px 10px;
    border-radius: var(--radius-md);
    border: 1px solid rgba(234, 179, 8, 0.4);
    background: rgba(234, 179, 8, 0.12);
  }

  .workspace-picker {
    border: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .workspace-picker legend {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--text-heading);
    padding: 0;
    margin-bottom: 6px;
  }

  .workspace-option {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 8px 10px;
    background: var(--bg-tertiary);
    border-radius: var(--radius-md);
    cursor: pointer;
  }

  .workspace-option input {
    margin-top: 2px;
    accent-color: var(--accent);
  }

  .workspace-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .workspace-name {
    font-size: var(--font-size-sm);
    color: var(--text-primary);
    font-weight: 500;
  }

  .workspace-help {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
  }

  .consent-hint {
    color: var(--text-muted);
    font-size: var(--font-size-xs);
    margin: 0;
  }

  .consent-disclaimer {
    color: var(--text-muted);
    font-size: var(--font-size-xs);
    margin: 0;
  }

  .consent-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 8px;
  }

  .btn-primary {
    padding: 8px 16px;
    border-radius: var(--radius-md);
    background: var(--accent);
    color: white;
    font-size: var(--font-size-sm);
    font-weight: 500;
    border: none;
    cursor: pointer;
  }
  .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-primary:not(:disabled):hover { opacity: 0.9; }

  .btn-ghost {
    padding: 8px 16px;
    border-radius: var(--radius-md);
    background: none;
    border: 1px solid var(--border-default);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }
  .btn-ghost:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-ghost:not(:disabled):hover { background: var(--bg-hover); }
</style>
