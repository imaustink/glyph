<script lang="ts">
  import { page } from '$app/state';
  import { browser } from '$app/environment';
  import { oauthConsentRepository } from '$lib/storage/repositories/ApiOAuthConsentRepository';
  import { UnauthorizedError, API_BASE } from '$lib/storage/apiClient';
  import type { OAuthConsentInfo, OAuthScope } from '$lib/models/types';

  let info = $state<OAuthConsentInfo | null>(null);
  let loading = $state(true);
  let error = $state('');
  let deciding = $state(false);

  function scopeReadable(scope: OAuthScope): string {
    const [resource, perm] = scope.split(':');
    const labels: Record<string, string> = {
      page: 'pages',
      task: 'tasks',
      template: 'templates',
      org: 'organization info'
    };
    const noun = labels[resource] ?? resource;
    return perm === 'write' ? `Edit ${noun}` : `View ${noun}`;
  }

  async function load() {
    if (!browser) return;
    loading = true;
    error = '';
    try {
      info = await oauthConsentRepository.getInfo(page.url.searchParams.toString());
    } catch (e) {
      if (e instanceof UnauthorizedError) {
        const next = encodeURIComponent(location.pathname + location.search);
        location.assign(`${API_BASE}/auth/login?next=${next}`);
        return;
      }
      error = e instanceof Error ? e.message : 'This authorization request is invalid.';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load();
  });

  async function decide(approve: boolean) {
    if (!info) return;
    deciding = true;
    try {
      const result = await oauthConsentRepository.decide(info.consentToken, approve);
      window.location.href = result.redirectUrl;
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to record your decision.';
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
      <p class="consent-org">for organization <strong>{info.orgName}</strong></p>

      <ul class="scope-list">
        {#each info.scopes as scope (scope)}
          <li>{scopeReadable(scope)}</li>
        {/each}
      </ul>

      <p class="consent-disclaimer">
        This will let {info.client.name} act on your behalf within {info.orgName}. You can revoke
        access at any time from organization settings.
      </p>

      <div class="consent-actions">
        <button class="btn-ghost" onclick={() => decide(false)} disabled={deciding}>Deny</button>
        <button class="btn-primary" onclick={() => decide(true)} disabled={deciding}>
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
