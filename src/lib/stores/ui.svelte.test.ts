/**
 * Unit tests for the ui store.
 *
 * Tests the save-state machine (markSaving/markSaved/waitForSaveComplete),
 * simple state setters, and the registerPendingFlush / waitForSaveComplete
 * interaction. Uses fake timers where necessary.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createUiStore } from './ui.svelte';

describe('uiStore', () => {
  let store: ReturnType<typeof createUiStore>;

  beforeEach(() => {
    vi.useFakeTimers();
    store = createUiStore();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // ─── simple state setters ─────────────────────────────────────────────────

  describe('setCurrentPage', () => {
    it('updates currentPageId', () => {
      store.setCurrentPage('page-1');
      expect(store.currentPageId).toBe('page-1');
    });

    it('accepts null', () => {
      store.setCurrentPage('page-1');
      store.setCurrentPage(null);
      expect(store.currentPageId).toBeNull();
    });
  });

  describe('toggleSidebar', () => {
    it('flips sidebarOpen from true to false', () => {
      expect(store.sidebarOpen).toBe(true);
      store.toggleSidebar();
      expect(store.sidebarOpen).toBe(false);
    });

    it('flips sidebarOpen from false to true', () => {
      store.toggleSidebar(); // → false
      store.toggleSidebar(); // → true
      expect(store.sidebarOpen).toBe(true);
    });
  });

  describe('closeSidebar', () => {
    it('sets sidebarOpen to false', () => {
      store.closeSidebar();
      expect(store.sidebarOpen).toBe(false);
    });
  });

  describe('openSearch / closeSearch', () => {
    it('openSearch sets searchOpen to true', () => {
      store.openSearch();
      expect(store.searchOpen).toBe(true);
    });

    it('closeSearch sets searchOpen to false', () => {
      store.openSearch();
      store.closeSearch();
      expect(store.searchOpen).toBe(false);
    });
  });

  describe('setPendingTaskCreation', () => {
    it('sets pendingTaskCreation', () => {
      const pending = { nodeId: 'n1', bulletText: 'Buy milk', pageId: 'p1', resolve: vi.fn() };
      store.setPendingTaskCreation(pending);
      expect(store.pendingTaskCreation).toEqual(pending);
    });

    it('clears pendingTaskCreation when set to null', () => {
      const pending = { nodeId: 'n1', bulletText: 'Buy milk', pageId: 'p1', resolve: vi.fn() };
      store.setPendingTaskCreation(pending);
      store.setPendingTaskCreation(null);
      expect(store.pendingTaskCreation).toBeNull();
    });
  });

  describe('setShouldFocusTitle', () => {
    it('sets shouldFocusTitle to true', () => {
      store.setShouldFocusTitle(true);
      expect(store.shouldFocusTitle).toBe(true);
    });

    it('sets shouldFocusTitle to false', () => {
      store.setShouldFocusTitle(true);
      store.setShouldFocusTitle(false);
      expect(store.shouldFocusTitle).toBe(false);
    });
  });

  describe('isSaving getter', () => {
    it('is false when saveState is idle', () => {
      expect(store.isSaving).toBe(false);
    });

    it('is true when saveState becomes saving', async () => {
      store.markSaving();
      vi.advanceTimersByTime(500);
      await Promise.resolve(); // allow microtasks
      expect(store.isSaving).toBe(true);
    });

    it('is false after markSaved', () => {
      store.markSaving();
      store.markSaved();
      expect(store.isSaving).toBe(false);
    });
  });

  // ─── save-state machine ───────────────────────────────────────────────────

  describe('markSaving / markSaved', () => {
    it('hasPendingWrites is false initially', () => {
      expect(store.hasPendingWrites).toBe(false);
    });

    it('hasPendingWrites is true after markSaving', () => {
      store.markSaving();
      expect(store.hasPendingWrites).toBe(true);
    });

    it('hasPendingWrites returns to false after markSaved', () => {
      store.markSaving();
      store.markSaved();
      expect(store.hasPendingWrites).toBe(false);
    });

    it('saveState is "idle" initially', () => {
      expect(store.saveState).toBe('idle');
    });

    it('saveState transitions to "saving" after 500 ms debounce', () => {
      store.markSaving();
      expect(store.saveState).toBe('idle'); // not yet
      vi.advanceTimersByTime(500);
      expect(store.saveState).toBe('saving');
    });

    it('saveState does not transition to "saving" if markSaved is called before 500 ms', () => {
      store.markSaving();
      vi.advanceTimersByTime(499);
      store.markSaved();
      vi.advanceTimersByTime(1); // debounce fires but should be suppressed
      expect(store.saveState).toBe('saved');
    });

    it('saveState transitions to "saved" when all in-flight saves complete', () => {
      store.markSaving();
      store.markSaved();
      expect(store.saveState).toBe('saved');
    });

    it('saveState returns to "idle" after 2000 ms in "saved"', () => {
      store.markSaving();
      store.markSaved();
      expect(store.saveState).toBe('saved');
      vi.advanceTimersByTime(2000);
      expect(store.saveState).toBe('idle');
    });

    it('tracks multiple concurrent in-flight saves correctly', () => {
      store.markSaving();
      store.markSaving();
      store.markSaved(); // 1 remaining
      expect(store.hasPendingWrites).toBe(true);
      store.markSaved(); // 0 remaining
      expect(store.hasPendingWrites).toBe(false);
      expect(store.saveState).toBe('saved');
    });

    it('markSaved does not go below zero for in-flight count', () => {
      // Should not throw even when called more times than markSaving
      store.markSaved();
      expect(store.hasPendingWrites).toBe(false);
    });

    it('markSaving clears _savedTimer when called during "saved" idle countdown (covers line 54)', () => {
      // Complete a save cycle so _savedTimer (2s idle) is active
      store.markSaving();
      store.markSaved();
      expect(store.saveState).toBe('saved');

      // Start a new save while the 2s timer is running → line 54 clears _savedTimer
      store.markSaving();

      // Advance past 2000ms — if _savedTimer wasn't cleared it would set state to 'idle'
      vi.advanceTimersByTime(2000);
      // saveState should NOT be 'idle' (the 2s timer was cancelled by markSaving)
      // It will be 'saving' after the 500ms debounce
      vi.advanceTimersByTime(500);
      expect(store.saveState).toBe('saving');
    });

    it('markSaved clears previous _savedTimer before setting a new one (covers line 71)', () => {
      // Scenario: markSaved is called when _inflightCount is already 0 (extra call)
      // This can happen when concurrent save completions race.
      store.markSaving();
      store.markSaved();  // _inflightCount=0, _savedTimer set (2s idle)
      expect(store.saveState).toBe('saved');

      // Call markSaved again (extra): _inflightCount = max(0, 0-1) = 0 → enters if block
      // line 71: _savedTimer is non-null → clearTimeout fires (true branch)
      store.markSaved();

      // Only one timer should be active — advance past 2s and verify idle
      vi.advanceTimersByTime(2000);
      expect(store.saveState).toBe('idle');
    });
  });

  // ─── waitForSaveComplete ──────────────────────────────────────────────────

  describe('waitForSaveComplete', () => {
    it('resolves immediately when there are no in-flight saves', async () => {
      await expect(store.waitForSaveComplete()).resolves.toBeUndefined();
    });

    it('resolves when markSaved is called after waiting begins', async () => {
      store.markSaving();
      const waitPromise = store.waitForSaveComplete();
      store.markSaved();
      await expect(waitPromise).resolves.toBeUndefined();
    });

    it('rejects after timeoutMs when saves never complete', async () => {
      store.markSaving();
      const waitPromise = store.waitForSaveComplete(1000);
      vi.advanceTimersByTime(1000);
      await expect(waitPromise).rejects.toThrow('save timeout');
    });

    it('does not reject if saves complete before the timeout', async () => {
      store.markSaving();
      const waitPromise = store.waitForSaveComplete(2000);
      vi.advanceTimersByTime(500);
      store.markSaved();
      await expect(waitPromise).resolves.toBeUndefined();
    });
  });

  // ─── registerPendingFlush ─────────────────────────────────────────────────

  describe('registerPendingFlush', () => {
    it('hasPendingWrites is true while a flush is pending', () => {
      let resolveFn!: () => void;
      const p = new Promise<void>((r) => { resolveFn = r; });
      store.registerPendingFlush(p);
      expect(store.hasPendingWrites).toBe(true);
      resolveFn();
    });

    it('hasPendingWrites becomes false after the flush resolves', async () => {
      let resolveFn!: () => void;
      const p = new Promise<void>((r) => { resolveFn = r; });
      store.registerPendingFlush(p);
      resolveFn();
      await p; // let microtasks settle
      // The `.finally` in registerPendingFlush removes the flush
      // Give microtasks a chance to run
      await Promise.resolve();
      expect(store.hasPendingWrites).toBe(false);
    });

    it('waitForSaveComplete resolves after pending flush completes (no in-flight saves)', async () => {
      let resolveFn!: () => void;
      const p = new Promise<void>((r) => { resolveFn = r; });
      store.registerPendingFlush(p);

      // _inflightCount === 0 but _pendingFlushes.length > 0
      // waitForSaveComplete should wait for the flush
      const waitPromise = store.waitForSaveComplete(2000);

      // Resolve the flush
      resolveFn();
      await p;
      await Promise.resolve(); // let finally() run to remove flush
      await Promise.resolve(); // let afterFlushes.then() run

      await expect(waitPromise).resolves.toBeUndefined();
    });

    it('hasPendingWrites is true from pending flush alone (no saving)', () => {
      let resolveFn!: () => void;
      const p = new Promise<void>((r) => { resolveFn = r; });
      store.registerPendingFlush(p);
      // No markSaving() called — _inflightCount === 0
      // hasPendingWrites should still be true via _pendingFlushes
      expect(store.hasPendingWrites).toBe(true);
      resolveFn(); // cleanup
    });
  });
});
