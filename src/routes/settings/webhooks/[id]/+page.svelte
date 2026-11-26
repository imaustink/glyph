<script lang="ts">
  import { browser } from '$app/environment';
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import { format, formatDistanceToNow, parseISO } from 'date-fns';
  import { repositories } from '$lib/storage/config';
  import { apiErrorMessage, handleAuthError } from '$lib/storage/apiClient';
  import { orgsStore } from '$lib/stores/orgs.svelte';
  import WebhookEventTypePicker from '$lib/components/webhooks/WebhookEventTypePicker.svelte';
  import WebhookSecret from '$lib/components/webhooks/WebhookSecret.svelte';
  import type { Webhook, WebhookEvent, WebhookEventType } from '$lib/models/types';

  const PAGE_SIZE = 50;
  const repo = repositories.webhooks;
  const id = $derived(page.params.id!);

  let hook = $state<Webhook | null>(null);
  let loading = $state(true);
  let loadError = $state('');

  // Settings form
  let name = $state('');
  let url = $state('');
  let eventTypes = $state<WebhookEventType[]>([]);
  let saving = $state(false);
  let saveError = $state('');
  let saved = $state(false);
  let rotatedSecret = $state<string | null>(null);

  // Events
  let events = $state<WebhookEvent[]>([]);
  let total = $state(0);
  let retentionDays = $state(30);
  let eventsLoading = $state(false);
  let eventsError = $state('');
  let expandedId = $state<string | null>(null);
  let replayingId = $state<string | null>(null);

  function syncForm(h: Webhook) {
    name = h.name;
    url = h.url;
    eventTypes = [...h.eventTypes];
  }

  async function load(webhookId: string) {
    if (!browser || !repo) return;
    loading = true;
    loadError = '';
    try {
      hook = await repo.get(webhookId);
      syncForm(hook);
      await loadEvents(true);
    } catch (e) {
      try {
        handleAuthError(e);
      } catch {
        loadError = apiErrorMessage(e, 'Failed to load webhook');
      }
    } finally {
      loading = false;
    }
  }

  async function loadEvents(reset: boolean) {
    if (!repo) return;
    eventsLoading = true;
    eventsError = '';
    try {
      const res = await repo.listEvents(id, { limit: PAGE_SIZE, offset: reset ? 0 : events.length });
      events = reset ? res.events : [...events, ...res.events];
      total = res.total;
      retentionDays = res.retentionDays;
    } catch (e) {
      eventsError = apiErrorMessage(e, 'Failed to load events');
    } finally {
      eventsLoading = false;
    }
  }

  $effect(() => {
    load(id);
    orgsStore.load();
  });

  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (!repo || !hook) return;
    saving = true;
    saveError = '';
    saved = false;
    try {
      hook = await repo.update(hook.id, { name: name.trim(), url: url.trim(), eventTypes });
      syncForm(hook);
      saved = true;
    } catch (err) {
      saveError = apiErrorMessage(err, 'Failed to save webhook');
    } finally {
      saving = false;
    }
  }

  async function toggleActive() {
    if (!repo || !hook) return;
    saveError = '';
    try {
      hook = await repo.update(hook.id, { active: !hook.active });
    } catch (err) {
      saveError = apiErrorMessage(err, 'Failed to update webhook');
    }
  }

  async function rotate() {
    if (!repo || !hook) return;
    if (!confirm('Rotate the signing secret? Deliveries are signed with the new secret immediately.')) return;
    saveError = '';
    try {
      const res = await repo.rotateSecret(hook.id);
      rotatedSecret = res.secret;
    } catch (err) {
      saveError = apiErrorMessage(err, 'Failed to rotate secret');
    }
  }

  async function remove() {
    if (!repo || !hook) return;
    if (!confirm(`Delete ${hook.name || hook.url}? Its event history is deleted too.`)) return;
    try {
      await repo.remove(hook.id);
      goto('/settings/webhooks');
    } catch (err) {
      saveError = apiErrorMessage(err, 'Failed to delete webhook');
    }
  }

  async function replay(ev: WebhookEvent) {
    if (!repo) return;
    replayingId = ev.id;
    eventsError = '';
    try {
      const updated = await repo.replay(id, ev.id);
      events = events.map((x) => (x.id === updated.id ? updated : x));
    } catch (err) {
      eventsError = apiErrorMessage(err, 'Failed to replay event');
    } finally {
      replayingId = null;
    }
  }

  const orgLabel = $derived(
    hook?.orgId ? (orgsStore.orgs.find((o) => o.id === hook?.orgId)?.name ?? 'Organization') : 'Personal'
  );

  function formatTime(iso: string): string {
    return format(parseISO(iso), 'MMM d, HH:mm:ss');
  }

  function relative(iso: string): string {
    return formatDistanceToNow(parseISO(iso), { addSuffix: true });
  }

  function statusLabel(ev: WebhookEvent): string {
    if (ev.status === 'pending' && ev.attempts > 0) return 'Retrying';
    return ev.status[0].toUpperCase() + ev.status.slice(1);
  }

  function codeClass(code: number | null): string {
    if (code === null) return 'code-none';
    if (code >= 200 && code < 300) return 'code-ok';
    return 'code-bad';
  }
</script>

<div class="webhook-page">
  <a class="back-link" href="/settings/webhooks">← Webhooks</a>

  {#if !repo}
    <p class="empty-state">Webhooks are only available in API mode.</p>
  {:else if loading}
    <p class="empty-state">Loading…</p>
  {:else if loadError || !hook}
    <p class="field-error">{loadError || 'Webhook not found'}</p>
  {:else}
    <div class="page-header">
      <div class="title-row">
        <h1>{hook.name || hook.url}</h1>
        {#if hook.active}
          <span class="badge badge-active">Active</span>
        {:else}
          <span class="badge badge-paused">Paused</span>
        {/if}
      </div>
      <p class="subtitle">{orgLabel} workspace · created {relative(hook.createdAt)}</p>
    </div>

    <section class="card">
      <h2>Settings</h2>
      <form class="settings-form" onsubmit={save}>
        <label class="field">
          <span>Name</span>
          <input type="text" bind:value={name} maxlength="100" />
        </label>
        <label class="field">
          <span>Endpoint URL</span>
          <input type="url" bind:value={url} required />
        </label>
        <div class="field">
          <span>Events</span>
          <WebhookEventTypePicker bind:value={eventTypes} />
        </div>
        {#if saveError}<p class="field-error">{saveError}</p>{/if}
        {#if saved}<p class="field-ok">Saved.</p>{/if}
        <div class="actions">
          <button class="btn-primary" type="submit" disabled={saving || !url.trim() || eventTypes.length === 0}>
            {saving ? 'Saving…' : 'Save changes'}
          </button>
          <button class="btn-secondary" type="button" onclick={toggleActive}>
            {hook.active ? 'Pause' : 'Resume'}
          </button>
          <button class="btn-secondary" type="button" onclick={rotate}>Rotate secret</button>
          <button class="btn-danger" type="button" onclick={remove}>Delete</button>
        </div>
      </form>
      {#if rotatedSecret}
        <div class="rotated">
          <p>New signing secret — copy it now, it won't be shown again:</p>
          <WebhookSecret secret={rotatedSecret} />
        </div>
      {/if}
    </section>

    <section class="card">
      <div class="events-header">
        <h2>Events</h2>
        <button class="btn-secondary" onclick={() => loadEvents(true)} disabled={eventsLoading}>Refresh</button>
      </div>
      <p class="hint">
        The last {retentionDays} days of events ({total}). Older events are deleted automatically.
      </p>
      {#if eventsError}<p class="field-error">{eventsError}</p>{/if}

      {#if events.length === 0 && !eventsLoading}
        <p class="empty-state">No events in the last {retentionDays} days.</p>
      {:else}
        <div class="table-wrap">
          <table class="events-table">
            <thead>
              <tr>
                <th>#</th>
                <th>Event</th>
                <th>Created</th>
                <th>Status</th>
                <th>Response</th>
                <th>Attempts</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {#each events as ev (ev.id)}
                <tr class="event-row" class:expanded={expandedId === ev.id}>
                  <td class="seq">{ev.sequence ?? '—'}</td>
                  <td>
                    <button
                      class="event-toggle"
                      onclick={() => (expandedId = expandedId === ev.id ? null : ev.id)}
                      aria-expanded={expandedId === ev.id}
                    >
                      <code>{ev.type}</code>
                    </button>
                  </td>
                  <td title={ev.createdAt}>{formatTime(ev.createdAt)}</td>
                  <td>
                    <span class="status status-{ev.status}">{statusLabel(ev)}</span>
                  </td>
                  <td>
                    <span class={codeClass(ev.lastStatusCode)} title={ev.lastError ?? ''}>
                      {ev.lastStatusCode ?? (ev.lastError ? 'No response' : '—')}
                    </span>
                  </td>
                  <td>{ev.attempts}</td>
                  <td class="row-actions">
                    <button
                      class="btn-secondary btn-small"
                      onclick={() => replay(ev)}
                      disabled={replayingId === ev.id || ev.status === 'pending'}
                      title={ev.status === 'pending'
                        ? 'Still queued for delivery; replay is available once it is delivered or failed'
                        : undefined}
                    >
                      {replayingId === ev.id ? '…' : 'Replay'}
                    </button>
                  </td>
                </tr>
                {#if expandedId === ev.id}
                  <tr class="detail-row">
                    <td colspan="7">
                      <dl class="event-details">
                        <dt>Event ID</dt>
                        <dd><code>{ev.id}</code></dd>
                        {#if ev.lastAttemptAt}
                          <dt>Last attempt</dt>
                          <dd>{formatTime(ev.lastAttemptAt)}</dd>
                        {/if}
                        {#if ev.lastError}
                          <dt>Last error</dt>
                          <dd>{ev.lastError}</dd>
                        {/if}
                        {#if ev.status === 'pending' && ev.attempts > 0}
                          <dt>Next retry</dt>
                          <dd>{relative(ev.nextAttemptAt)}</dd>
                        {/if}
                        {#if ev.deliveredAt}
                          <dt>Delivered</dt>
                          <dd>{formatTime(ev.deliveredAt)}</dd>
                        {/if}
                      </dl>
                      <pre class="payload">{JSON.stringify(ev.data, null, 2)}</pre>
                    </td>
                  </tr>
                {/if}
              {/each}
            </tbody>
          </table>
        </div>
        {#if events.length < total}
          <button class="btn-secondary load-more" onclick={() => loadEvents(false)} disabled={eventsLoading}>
            {eventsLoading ? 'Loading…' : 'Load more'}
          </button>
        {/if}
      {/if}
    </section>
  {/if}
</div>

<style>
  .webhook-page {
    padding: 32px 40px;
    max-width: 960px;
    margin: 0 auto;
  }

  .back-link {
    display: inline-block;
    margin-bottom: 16px;
    color: var(--text-muted);
    font-size: var(--font-size-sm);
    text-decoration: none;
  }

  .page-header {
    margin-bottom: 20px;
  }

  .title-row {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }

  .title-row h1 {
    font-size: 1.6em;
    font-weight: 700;
    color: var(--text-heading);
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .subtitle,
  .hint {
    color: var(--text-muted);
    font-size: var(--font-size-sm);
    margin: 4px 0 0;
  }

  .card {
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-lg);
    padding: 16px 20px;
    margin-bottom: 20px;
  }

  h2 {
    font-size: var(--font-size-md);
    font-weight: 600;
    color: var(--text-heading);
    margin: 0 0 12px;
  }

  .settings-form {
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

  .field input {
    padding: 8px 10px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border-default);
    background: var(--bg-primary);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
  }

  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .rotated {
    margin-top: 12px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: var(--font-size-sm);
  }

  .rotated p {
    margin: 0;
    color: var(--text-primary);
  }

  .events-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .events-header h2 {
    margin: 0;
  }

  .table-wrap {
    overflow-x: auto;
    margin-top: 12px;
  }

  .events-table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--font-size-sm);
  }

  .events-table th {
    text-align: left;
    font-weight: 500;
    color: var(--text-muted);
    font-size: var(--font-size-xs);
    padding: 6px 8px;
    border-bottom: 1px solid var(--border-subtle);
    white-space: nowrap;
  }

  .events-table td {
    padding: 6px 8px;
    border-bottom: 1px solid var(--border-subtle);
    color: var(--text-primary);
    white-space: nowrap;
    vertical-align: middle;
  }

  .seq {
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }

  .event-toggle {
    background: none;
    border: none;
    padding: 0;
    color: var(--text-link, inherit);
    cursor: pointer;
    font: inherit;
  }

  .event-row.expanded td {
    border-bottom: none;
  }

  .detail-row td {
    white-space: normal;
    background: var(--bg-primary);
  }

  .event-details {
    display: grid;
    grid-template-columns: 110px 1fr;
    gap: 4px 12px;
    margin: 4px 0 8px;
    font-size: var(--font-size-xs);
  }

  .event-details dt {
    color: var(--text-muted);
  }

  .event-details dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  .payload {
    margin: 0;
    padding: 8px;
    border-radius: var(--radius-md);
    background: var(--bg-tertiary);
    font-size: var(--font-size-xs);
    max-height: 320px;
    overflow: auto;
  }

  .status {
    font-size: var(--font-size-xs);
    padding: 2px 6px;
    border-radius: 9999px;
    font-weight: 500;
  }
  .status-delivered { background: rgba(34, 197, 94, 0.15); color: #16a34a; }
  .status-pending { background: rgba(234, 179, 8, 0.15); color: #ca8a04; }
  .status-failed { background: rgba(239, 68, 68, 0.15); color: #dc2626; }

  .code-ok { color: #16a34a; font-variant-numeric: tabular-nums; }
  .code-bad { color: #dc2626; font-variant-numeric: tabular-nums; }
  .code-none { color: var(--text-muted); }

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

  .btn-secondary,
  .btn-danger {
    padding: 6px 12px;
    border-radius: var(--radius-md);
    background: none;
    border: 1px solid var(--border-default);
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    cursor: pointer;
    white-space: nowrap;
  }

  .btn-danger {
    color: var(--text-danger, #f87171);
  }

  .btn-small {
    padding: 3px 8px;
    font-size: var(--font-size-xs);
  }

  .btn-primary:disabled,
  .btn-secondary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-secondary:not(:disabled):hover,
  .btn-danger:hover {
    background: var(--bg-hover);
  }

  .load-more {
    margin-top: 12px;
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

  .field-ok {
    color: #16a34a;
    font-size: var(--font-size-xs);
    margin: 0;
  }

  /* Clear the fixed hamburger button (top:10 + height:44 = 54px), shown at <=768px */
  @media (max-width: 768px) {
    .webhook-page { padding: 16px 40px 32px; }
  }

  @media (max-width: 640px) {
    .webhook-page { padding: 16px 16px 16px; }
    .field input { font-size: 16px; }
    .event-details { grid-template-columns: 1fr; }
  }
</style>
