<script lang="ts">
  import { browser } from '$app/environment';
  import { goto } from '$app/navigation';
  import { repositories } from '$lib/storage/config';
  import { apiErrorMessage, handleAuthError } from '$lib/storage/apiClient';
  import { orgsStore } from '$lib/stores/orgs.svelte';
  import WebhookEventTypePicker from '$lib/components/webhooks/WebhookEventTypePicker.svelte';
  import WebhookSecret from '$lib/components/webhooks/WebhookSecret.svelte';
  import { WEBHOOK_EVENT_TYPES } from '$lib/models/types';
  import type { Webhook, WebhookEventType, WebhookWithSecret } from '$lib/models/types';

  const repo = repositories.webhooks;

  let webhooks = $state<Webhook[]>([]);
  let loading = $state(true);
  let error = $state('');

  // Create form
  let name = $state('');
  let url = $state('');
  let eventTypes = $state<WebhookEventType[]>([...WEBHOOK_EVENT_TYPES]);
  let scope = $state(''); // '' = personal, else an org id
  let creating = $state(false);
  let createError = $state('');
  let created = $state<WebhookWithSecret | null>(null);

  const ownedOrgs = $derived(orgsStore.orgs.filter((o) => o.role === 'owner'));
  const orgName = (id: string | null) =>
    id ? (orgsStore.orgs.find((o) => o.id === id)?.name ?? 'Organization') : 'Personal';

  async function load() {
    if (!browser || !repo) return;
    loading = true;
    error = '';
    try {
      webhooks = await repo.list();
    } catch (e) {
      try {
        handleAuthError(e);
      } catch {
        error = apiErrorMessage(e, 'Failed to load webhooks');
      }
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load();
    orgsStore.load();
  });

  async function create(e: SubmitEvent) {
    e.preventDefault();
    if (!repo || creating) return;
    creating = true;
    createError = '';
    try {
      const hook = await repo.create({
        name: name.trim(),
        url: url.trim(),
        eventTypes,
        orgId: scope || null
      });
      created = hook;
      webhooks = [...webhooks, hook];
      name = '';
      url = '';
      eventTypes = [...WEBHOOK_EVENT_TYPES];
      scope = '';
    } catch (err) {
      createError = apiErrorMessage(err, 'Failed to create webhook');
    } finally {
      creating = false;
    }
  }
</script>

<div class="webhooks-page">
  <div class="page-header">
    <h1>Webhooks</h1>
    <p class="subtitle">
      Send signed HTTP requests to your own endpoints when notes or tasks are created, updated or deleted.
    </p>
  </div>

  {#if !repo}
    <p class="empty-state">Webhooks are only available in API mode.</p>
  {:else}
    {#if created}
      <div class="created-banner">
        <p>
          <strong>{created.name || created.url}</strong> was created. Copy its signing secret now — it won't be
          shown again.
        </p>
        <WebhookSecret secret={created.secret} />
        <div class="banner-actions">
          <a class="btn-secondary" href="/settings/webhooks/{created.id}">View events</a>
          <button class="btn-link" onclick={() => (created = null)}>Done</button>
        </div>
      </div>
    {/if}

    <section class="card">
      <h2>New webhook</h2>
      <form class="create-form" onsubmit={create}>
        <label class="field">
          <span>Name</span>
          <input type="text" bind:value={name} maxlength="100" placeholder="e.g. Zapier" />
        </label>
        <label class="field">
          <span>Endpoint URL</span>
          <input type="url" bind:value={url} required placeholder="https://example.com/glyph-webhook" />
        </label>
        <label class="field">
          <span>Workspace</span>
          <select bind:value={scope}>
            <option value="">Personal — my notes and tasks</option>
            {#each ownedOrgs as org (org.id)}
              <option value={org.id}>{org.name} — the organization's shared notes and tasks</option>
            {/each}
          </select>
        </label>
        <div class="field">
          <span>Events</span>
          <WebhookEventTypePicker bind:value={eventTypes} />
        </div>
        {#if createError}<p class="field-error">{createError}</p>{/if}
        <div>
          <button class="btn-primary" type="submit" disabled={creating || !url.trim() || eventTypes.length === 0}>
            {creating ? 'Creating…' : 'Create webhook'}
          </button>
        </div>
      </form>
    </section>

    {#if loading}
      <p class="empty-state">Loading…</p>
    {:else}
      {#if error}<p class="field-error">{error}</p>{/if}
      {#if webhooks.length === 0}
        <p class="empty-state">You have no webhooks yet.</p>
      {:else}
        <ul class="webhook-list">
          {#each webhooks as hook (hook.id)}
            <li>
              <button class="webhook-row" onclick={() => goto(`/settings/webhooks/${hook.id}`)}>
                <span class="webhook-main">
                  <span class="webhook-name">{hook.name || hook.url}</span>
                  <span class="webhook-url">{hook.url}</span>
                </span>
                <span class="webhook-meta">
                  <span class="chip">{orgName(hook.orgId)}</span>
                  <span class="chip">{hook.eventTypes.length} event{hook.eventTypes.length === 1 ? '' : 's'}</span>
                  {#if hook.active}
                    <span class="badge badge-active">Active</span>
                  {:else}
                    <span class="badge badge-paused">Paused</span>
                  {/if}
                </span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .webhooks-page {
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

  .card,
  .created-banner {
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-lg);
    padding: 16px 20px;
    margin-bottom: 20px;
  }

  .created-banner {
    border-color: var(--accent, #6366f1);
    display: flex;
    flex-direction: column;
    gap: 10px;
    font-size: var(--font-size-sm);
  }

  .created-banner p {
    margin: 0;
    color: var(--text-primary);
  }

  .banner-actions {
    display: flex;
    gap: 12px;
    align-items: center;
  }

  h2 {
    font-size: var(--font-size-md);
    font-weight: 600;
    color: var(--text-heading);
    margin: 0 0 12px;
  }

  .create-form {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: var(--font-size-sm);
  }

  .field > span {
    color: var(--text-muted);
  }

  .field input,
  .field select {
    padding: 8px 10px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border-default);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
  }

  .webhook-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .webhook-row {
    width: 100%;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-lg);
    color: inherit;
    text-align: left;
    cursor: pointer;
    font: inherit;
  }

  .webhook-row:hover {
    background: var(--bg-hover);
  }

  .webhook-main {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .webhook-name {
    font-weight: 600;
    color: var(--text-heading);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .webhook-url {
    font-size: var(--font-size-xs);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .webhook-meta {
    display: flex;
    gap: 6px;
    flex-shrink: 0;
    align-items: center;
  }

  .chip {
    font-size: var(--font-size-xs);
    padding: 2px 8px;
    border-radius: 9999px;
    background: var(--bg-tertiary);
    color: var(--text-primary);
    white-space: nowrap;
  }

  .badge {
    font-size: var(--font-size-xs);
    padding: 2px 6px;
    border-radius: 9999px;
    font-weight: 500;
    white-space: nowrap;
  }
  .badge-active { background: rgba(34, 197, 94, 0.15); color: #16a34a; }
  .badge-paused { background: rgba(148, 163, 184, 0.2); color: var(--text-muted); }

  .btn-primary {
    padding: 8px 14px;
    border-radius: var(--radius-md);
    background: var(--accent, #6366f1);
    border: none;
    color: var(--accent-contrast, #fff);
    font-size: var(--font-size-sm);
    cursor: pointer;
  }
  .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }

  .btn-secondary {
    padding: 6px 12px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border-default);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    text-decoration: none;
  }

  .btn-link {
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    font-size: var(--font-size-sm);
  }

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

  /* Clear the fixed hamburger button (top:10 + height:44 = 54px), shown at <=768px */
  @media (max-width: 768px) {
    .webhooks-page { padding: 16px 40px 32px; }
  }

  @media (max-width: 640px) {
    .webhooks-page { padding: 16px 16px 16px; }
    .webhook-row { flex-direction: column; align-items: flex-start; }
    .webhook-meta { flex-wrap: wrap; }
    .field input,
    .field select { font-size: 16px; }
  }
</style>
