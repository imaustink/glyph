/**
 * Unit tests for the notifications store.
 *
 * Uses fake timers to test auto-dismiss and manual dismiss without wall-clock delays.
 * Creates a fresh store instance per test to avoid state leakage.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createNotificationsStore } from './notifications.svelte';

describe('notificationsStore', () => {
  let store: ReturnType<typeof createNotificationsStore>;

  beforeEach(() => {
    vi.useFakeTimers();
    store = createNotificationsStore();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // ─── add / shortcut helpers ───────────────────────────────────────────────

  describe('success / error / warning / info shortcuts', () => {
    it('success() adds a notification with type "success"', () => {
      store.success('All good');
      expect(store.notifications).toHaveLength(1);
      expect(store.notifications[0].type).toBe('success');
      expect(store.notifications[0].message).toBe('All good');
    });

    it('error() adds a notification with type "error"', () => {
      store.error('Something broke');
      expect(store.notifications[0].type).toBe('error');
    });

    it('warning() adds a notification with type "warning"', () => {
      store.warning('Be careful');
      expect(store.notifications[0].type).toBe('warning');
    });

    it('info() adds a notification with type "info"', () => {
      store.info('FYI');
      expect(store.notifications[0].type).toBe('info');
    });

    it('each shortcut returns the new notification id', () => {
      const id = store.success('Hi');
      expect(typeof id).toBe('string');
      expect(id.length).toBeGreaterThan(0);
    });
  });

  // ─── dismissible auto-dismiss ─────────────────────────────────────────────

  describe('auto-dismiss (dismissible = true)', () => {
    it('notification is present immediately after add', () => {
      store.success('hello');
      expect(store.notifications).toHaveLength(1);
    });

    it('auto-dismisses after 5000 ms', () => {
      store.success('bye');
      vi.advanceTimersByTime(5000);
      expect(store.notifications).toHaveLength(0);
    });

    it('does not auto-dismiss before 5000 ms', () => {
      store.success('still here');
      vi.advanceTimersByTime(4999);
      expect(store.notifications).toHaveLength(1);
    });

    it('multiple notifications each have their own timers', () => {
      store.success('first');
      vi.advanceTimersByTime(2500);
      store.success('second');
      vi.advanceTimersByTime(2500); // first timer fires, second has 2500 ms left
      expect(store.notifications).toHaveLength(1);
      expect(store.notifications[0].message).toBe('second');
      vi.advanceTimersByTime(2500);
      expect(store.notifications).toHaveLength(0);
    });
  });

  // ─── dismiss() ───────────────────────────────────────────────────────────

  describe('dismiss(id)', () => {
    it('removes the notification immediately', () => {
      const id = store.success('remove me');
      store.dismiss(id);
      expect(store.notifications).toHaveLength(0);
    });

    it('cancels the auto-dismiss timer so it does not fire later', () => {
      const id = store.success('cancel timer');
      store.dismiss(id);
      // Advancing time should not cause any error or duplicate removal
      vi.advanceTimersByTime(5000);
      expect(store.notifications).toHaveLength(0);
    });

    it('is a no-op for an unknown id', () => {
      store.success('other');
      store.dismiss('unknown-id');
      expect(store.notifications).toHaveLength(1);
    });
  });

  // ─── non-dismissible ─────────────────────────────────────────────────────

  describe('non-dismissible notifications (dismissible = false)', () => {
    it('persists after 5000 ms (does not auto-dismiss)', () => {
      store.add('info', 'permanent notice', false);
      expect(store.notifications).toHaveLength(1);
      vi.advanceTimersByTime(10_000);
      expect(store.notifications).toHaveLength(1);
    });

    it('can still be manually dismissed', () => {
      const id = store.add('warning', 'sticky', false);
      store.dismiss(id);
      expect(store.notifications).toHaveLength(0);
    });
  });

  // ─── clear() ─────────────────────────────────────────────────────────────

  describe('clear()', () => {
    it('removes all notifications', () => {
      store.success('one');
      store.error('two');
      store.info('three');
      store.clear();
      expect(store.notifications).toHaveLength(0);
    });

    it('cancels all pending timers so they do not fire after clear', () => {
      store.success('a');
      store.success('b');
      store.clear();
      vi.advanceTimersByTime(5000);
      expect(store.notifications).toHaveLength(0);
    });

    it('is safe to call on an already-empty store', () => {
      expect(() => store.clear()).not.toThrow();
    });
  });
});
