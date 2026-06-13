import type { PendingTaskCreation } from '$lib/models/types';

export type SaveState = 'idle' | 'saving' | 'saved';

export function createUiStore() {
  let currentPageId = $state<string | null>(null);
  let sidebarOpen = $state(true);
  let searchOpen = $state(false);
  let pendingTaskCreation = $state<PendingTaskCreation | null>(null);
  let shouldFocusTitle = $state(false);
  let saveState = $state<SaveState>('idle');
  let _savedTimer: ReturnType<typeof setTimeout> | null = null;
  let _savingTimer: ReturnType<typeof setTimeout> | null = null;

  /** Number of in-flight save operations. When it drops to 0 we transition to 'saved'. */
  let _inflightCount = $state(0);

  /** Pending flush promises registered by destroyed components. */
  let _pendingFlushes = $state<Promise<void>[]>([]);

  function setCurrentPage(id: string | null) {
    currentPageId = id;
  }

  function toggleSidebar() {
    sidebarOpen = !sidebarOpen;
  }

  function closeSidebar() {
    sidebarOpen = false;
  }

  function openSearch() {
    searchOpen = true;
  }

  function closeSearch() {
    searchOpen = false;
  }

  function setPendingTaskCreation(pending: PendingTaskCreation | null) {
    pendingTaskCreation = pending;
  }

  function setShouldFocusTitle(focus: boolean) {
    shouldFocusTitle = focus;
  }

  /** Resolvers waiting for all saves to complete. */
  let _saveCompleteResolvers: (() => void)[] = [];

  /** Call before starting an async save. */
  function markSaving() {
    if (_savedTimer) { clearTimeout(_savedTimer); _savedTimer = null; }
    _inflightCount++;
    // Delay showing "Saving…" so quick round-trips don't flash the indicator
    if (!_savingTimer && saveState !== 'saving') {
      _savingTimer = setTimeout(() => {
        _savingTimer = null;
        // _inflightCount may have reached 0 if markSaved was called before this
        // timer fired but after the timer was already running (extremely rare race).
        /* c8 ignore next */
        if (_inflightCount > 0) saveState = 'saving';
      }, 500);
    }
  }

  /** Call after an async save resolves. Transitions to 'saved' when all in-flight saves finish. */
  function markSaved() {
    _inflightCount = Math.max(0, _inflightCount - 1);
    if (_inflightCount === 0) {
      if (_savingTimer) { clearTimeout(_savingTimer); _savingTimer = null; }
      saveState = 'saved';
      if (_savedTimer) clearTimeout(_savedTimer);
      _savedTimer = setTimeout(() => { saveState = 'idle'; }, 2000);
      // Notify all waiters that saves have completed
      const resolvers = _saveCompleteResolvers;
      _saveCompleteResolvers = [];
      resolvers.forEach(r => r());
    }
  }

  /**
   * Returns a promise that resolves when all in-flight saves and pending flushes complete,
   * or rejects after timeoutMs to prevent infinite waiting.
   */
  function waitForSaveComplete(timeoutMs = 5000): Promise<void> {
    if (_inflightCount === 0 && _pendingFlushes.length === 0) return Promise.resolve();

    return new Promise<void>((resolve, reject) => {
      const timeoutId = setTimeout(() => {
        // Drop our resolver so markSaved doesn't call it after we've already rejected.
        _saveCompleteResolvers = _saveCompleteResolvers.filter(r => r !== onSavesDone);
        reject(new Error('save timeout'));
      }, timeoutMs);

      function onSavesDone() {
        const afterFlushes = _pendingFlushes.length === 0
          ? Promise.resolve()
          : Promise.all([..._pendingFlushes]).then(() => {});
        afterFlushes.then(() => {
          clearTimeout(timeoutId);
          resolve();
        });
      }

      if (_inflightCount === 0) {
        onSavesDone();
      } else {
        _saveCompleteResolvers.push(onSavesDone);
      }
    });
  }

  /**
   * Register a flush promise from a destroyed component.
   * The navigation guard will wait for all registered flushes before proceeding.
   */
  function registerPendingFlush(p: Promise<void>) {
    _pendingFlushes = [..._pendingFlushes, p];
    p.finally(() => {
      _pendingFlushes = _pendingFlushes.filter(f => f !== p);
    });
  }

  return {
    get currentPageId() { return currentPageId; },
    get sidebarOpen() { return sidebarOpen; },
    get searchOpen() { return searchOpen; },
    get pendingTaskCreation() { return pendingTaskCreation; },
    get shouldFocusTitle() { return shouldFocusTitle; },
    get saveState() { return saveState; },
    get isSaving() { return saveState === 'saving'; },
    /** True when there are actual in-flight writes or pending flushes, regardless of display state. */
    get hasPendingWrites() { return _inflightCount > 0 || _pendingFlushes.length > 0; },
    setCurrentPage,
    toggleSidebar,
    closeSidebar,
    openSearch,
    closeSearch,
    setPendingTaskCreation,
    setShouldFocusTitle,
    markSaving,
    markSaved,
    waitForSaveComplete,
    registerPendingFlush
  };
}

export const uiStore = createUiStore();
