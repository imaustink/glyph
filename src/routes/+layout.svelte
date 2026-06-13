<script lang="ts">
  import '../app.css';
  import { onMount } from 'svelte';
  import { goto, beforeNavigate } from '$app/navigation';
  import { page } from '$app/state';
  import { pagesStore } from '$lib/stores/pages.svelte';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  import { lanesStore } from '$lib/stores/lanes.svelte';
  import { templatesStore } from '$lib/stores/templates.svelte';
  import { orgsStore } from '$lib/stores/orgs.svelte';
  import { authStore, type AuthLoadResult } from '$lib/stores/auth.svelte';
  import { evaluateTitleTemplate, evaluateContentTemplate } from '$lib/utils/titleTemplate';
  import { uiStore } from '$lib/stores/ui.svelte';
  import { storageMode } from '$lib/storage/config';
  import { API_BASE, handleAuthError } from '$lib/storage/apiClient';
  import { notificationsStore } from '$lib/stores/notifications.svelte';
  import { estimateStorageUsage, downloadExport } from '$lib/utils/export';
  import Sidebar from '$lib/components/sidebar/Sidebar.svelte';
  import SearchModal from '$lib/components/search/SearchModal.svelte';
  import ToastContainer from '$lib/components/shared/ToastContainer.svelte';

  let { children } = $props();
  let loadError = $state<string | null>(null);

  // ─── Storage quota tracking (local mode only) ──────────────────────────────
  let storageUsagePercent = $state(0);

  function refreshStorageUsage() {
    if (storageMode !== 'local') return;
    const usage = estimateStorageUsage();
    storageUsagePercent = usage ? Math.round(usage.percentage * 100) : 0;
  }

  // Re-check quota after each save completes.
  $effect(() => {
    if (uiStore.saveState === 'saved') refreshStorageUsage();
  });

  onMount(async () => {
    // In API mode, verify the session before loading data.
    // If not authenticated the /auth/me call returns 401 and
    // the apiClient redirects to the OIDC login page.
    if (storageMode === 'api') {
      const result: AuthLoadResult = await authStore.load();
      if (result === 'unauthenticated') {
        location.assign(`${API_BASE}/auth/login`);
        return;
      }
      if (result === 'network-error') {
        notificationsStore.error('Unable to connect to server. Please check your connection and reload.');
        return;
      }
    }

    try {
      await Promise.all([
        pagesStore.load(),
        tasksStore.load(),
        lanesStore.load(),
        templatesStore.load(),
        orgsStore.load()
      ]);
    } catch (err) {
      try {
        handleAuthError(err);
      } catch (nonAuthErr) {
        // handleAuthError re-throws non-auth errors
        loadError = nonAuthErr instanceof Error ? nonAuthErr.message : 'Failed to load data. Please check your connection and reload.';
        notificationsStore.error(loadError);
        return;
      }
    }

    // Seed defaults after all loads complete (idempotent)
    await Promise.all([
      lanesStore.seedDefaults(),
      templatesStore.seedDefaults()
    ]);

    // Check localStorage quota (local mode only) — drives the persistent banner
    if (storageMode === 'local') {
      refreshStorageUsage();
    }

    if (page.url.pathname === '/') {
      const pages = pagesStore.nodes.filter((n) => n.type === 'page');
      if (pages.length === 0) {
        const template = templatesStore.defaultTemplate;
        const content = template?.content ? evaluateContentTemplate(template.content) : undefined;
        const newPage = await pagesStore.createPage(null, 'Getting Started', content, template?.todoTrigger);
        goto(`/notes/${newPage.id}`);
      } else {
        goto(`/notes/${pages[0].id}`);
      }
    }
  });

  // Block SvelteKit client-side navigation while saves are in-flight.
  // The navigation is cancelled and retried once all pending writes resolve.
  let navRetrying = false;
  beforeNavigate((navigation) => {
    // Close sidebar on mobile when navigating
    if (window.innerWidth <= 768) {
      uiStore.closeSidebar();
    }

    // If we're already retrying navigation after a save flush, let it through
    if (navRetrying) return;

    if (uiStore.hasPendingWrites && navigation.to) {
      navigation.cancel();
      navRetrying = true;
      // Wait for saves with a 2-second timeout to prevent blocking the user
      uiStore.waitForSaveComplete(2000).finally(() => {
        navRetrying = false;
        goto(navigation.to!.url.pathname);
      });
    }
  });

  function handleKeydown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
      e.preventDefault();
      uiStore.openSearch();
    }
    if ((e.metaKey || e.ctrlKey) && e.key === 'n') {
      // Only intercept if not inside an input/textarea/contenteditable
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag !== 'INPUT' && tag !== 'TEXTAREA' && !(e.target as HTMLElement)?.isContentEditable) {
        e.preventDefault();
        const template = templatesStore.defaultTemplate;
        const title = template?.titleTemplate ? evaluateTitleTemplate(template.titleTemplate) : '';
        const content = template?.content ? evaluateContentTemplate(template.content) : undefined;
        const parentId = template?.defaultFolderId ?? null;
        pagesStore.createPage(parentId, title, content, template?.todoTrigger).then((p) => {
          uiStore.setShouldFocusTitle(true);
          goto(`/notes/${p.id}`);
        });
      }
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="app-shell">
  {#if loadError}
    <div class="error-screen">
      <p class="error-message">{loadError}</p>
      <button class="btn-primary" onclick={() => location.reload()}>Retry</button>
    </div>
  {:else if pagesStore.loaded}
    <!-- ─── Critical storage block (95%+): modal prevents all interaction ─── -->
    {#if storageUsagePercent >= 95}
      <div class="quota-block-overlay" role="dialog" aria-modal="true" aria-labelledby="quota-block-title">
        <div class="quota-block-dialog">
          <div class="quota-block-icon">⚠️</div>
          <h2 id="quota-block-title">Storage critically full ({storageUsagePercent}%)</h2>
          <p>
            Your browser storage is almost out of space. New saves will fail and you
            risk losing data. Export your notes now, then delete pages or switch to
            the API backend.
          </p>
          <div class="quota-block-actions">
            <button class="btn-primary" onclick={() => downloadExport()}>Export all data</button>
            <a href="/search" class="btn-ghost">Go to search to delete pages</a>
          </div>
        </div>
      </div>
    {/if}

    <!-- ─── Warning banner (80–94%): persistent, non-dismissable ─────────── -->
    {#if storageUsagePercent >= 80 && storageUsagePercent < 95}
      <div class="quota-banner" role="alert">
        <span class="quota-banner-text">
          ⚠️ Storage is <strong>{storageUsagePercent}% full</strong>. Export your data or switch to
          the API backend to avoid data loss.
        </span>
        <button class="btn-ghost quota-banner-btn" onclick={() => downloadExport()}>Export now</button>
      </div>
    {/if}

    <!-- Mobile sidebar overlay backdrop -->
    {#if uiStore.sidebarOpen}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sidebar-backdrop" onclick={() => uiStore.closeSidebar()} onkeydown={() => {}}></div>
    {/if}

    <div class="content-row">
      <div class="sidebar-container" class:open={uiStore.sidebarOpen}>
        <Sidebar />
      </div>

      <main class="main-content">
        <!-- Mobile header with hamburger menu -->
        <button class="mobile-menu-btn" onclick={() => uiStore.toggleSidebar()} aria-label="Toggle sidebar">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="3" y1="6" x2="21" y2="6"/>
            <line x1="3" y1="12" x2="21" y2="12"/>
            <line x1="3" y1="18" x2="21" y2="18"/>
          </svg>
        </button>
        {@render children()}
      </main>
    </div>
  {:else}
    <div class="loading-screen">
      <div class="loading-pulse"></div>
    </div>
  {/if}

  {#if uiStore.searchOpen}
    <SearchModal />
  {/if}

  <ToastContainer />
</div>

<style>
  .app-shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
    position: relative;
  }

  /* ─── Storage quota banner ───────────────────────────────────────────────── */
  .quota-banner {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 8px 16px;
    background: color-mix(in srgb, var(--priority-high) 20%, var(--bg-secondary));
    border-bottom: 1px solid color-mix(in srgb, var(--priority-high) 40%, transparent);
    color: var(--text-primary);
    font-size: 13px;
    flex-wrap: wrap;
  }

  .quota-banner-text {
    flex: 1;
    min-width: 200px;
  }

  .quota-banner-btn {
    white-space: nowrap;
    font-size: 12px;
    padding: 4px 10px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-sm);
  }

  /* ─── Critical quota block overlay ──────────────────────────────────────── */
  .quota-block-overlay {
    position: fixed;
    inset: 0;
    z-index: 9999;
    background: rgba(0, 0, 0, 0.8);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
  }

  .quota-block-dialog {
    background: var(--bg-modal);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    padding: 32px;
    max-width: 480px;
    width: 100%;
    text-align: center;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
  }

  .quota-block-icon {
    font-size: 48px;
  }

  .quota-block-dialog h2 {
    color: var(--text-heading);
    font-size: 20px;
    margin: 0;
  }

  .quota-block-dialog p {
    color: var(--text-secondary);
    font-size: 14px;
    line-height: 1.6;
    margin: 0;
  }

  .quota-block-actions {
    display: flex;
    gap: 12px;
    flex-wrap: wrap;
    justify-content: center;
    margin-top: 8px;
  }

  /* ─── Layout rows ────────────────────────────────────────────────────────── */
  .content-row {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  .sidebar-container {
    flex-shrink: 0;
  }

  .main-content {
    flex: 1;
    overflow: auto;
    min-width: 0;
    position: relative;
  }

  .sidebar-backdrop {
    display: none;
  }

  .mobile-menu-btn {
    display: none;
  }

  .loading-screen {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .error-screen {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;
    padding: 24px;
  }

  .error-message {
    color: var(--text-secondary);
    text-align: center;
    max-width: 400px;
  }

  .loading-pulse {
    width: 40px;
    height: 40px;
    border-radius: 50%;
    background: var(--accent-muted);
    animation: pulse 1.2s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { transform: scale(0.8); opacity: 0.5; }
    50% { transform: scale(1.1); opacity: 1; }
  }

  /* ─── Mobile responsive ─────────────────────────────────────────────────── */
  @media (max-width: 768px) {
    .sidebar-container {
      position: fixed;
      top: 0;
      left: 0;
      z-index: 50;
      transform: translateX(-100%);
      transition: transform 200ms ease;
    }

    .sidebar-container.open {
      transform: translateX(0);
    }

    .sidebar-backdrop {
      display: block;
      position: fixed;
      inset: 0;
      background: rgba(0, 0, 0, 0.5);
      z-index: 40;
    }

    .mobile-menu-btn {
      display: flex;
      align-items: center;
      justify-content: center;
      position: fixed;
      top: 10px;
      left: 10px;
      z-index: 30;
      width: 36px;
      height: 36px;
      padding: 0;
      border-radius: var(--radius-md);
      background: var(--bg-secondary);
      border: 1px solid var(--border-subtle);
      color: var(--text-secondary);
      cursor: pointer;
    }
    .mobile-menu-btn:hover {
      background: var(--bg-hover);
      color: var(--text-primary);
    }
  }
</style>
