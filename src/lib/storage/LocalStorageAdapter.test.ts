/**
 * Unit tests for LocalStorageAdapter.
 *
 * Vitest's jsdom environment provides a real localStorage mock, so these
 * tests exercise the full read/write/parse/quota-error paths without any
 * additional mocking.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { LocalStorageAdapter } from './LocalStorageAdapter';

describe('LocalStorageAdapter', () => {
  let adapter: LocalStorageAdapter;

  beforeEach(() => {
    adapter = new LocalStorageAdapter();
    localStorage.clear();
    vi.restoreAllMocks();
  });

  // ─── get ─────────────────────────────────────────────────────────────────────

  describe('get', () => {
    it('returns null when the key does not exist', async () => {
      expect(await adapter.get('missing')).toBeNull();
    });

    it('returns the parsed value when the key exists', async () => {
      localStorage.setItem('glyph:test', JSON.stringify({ a: 1 }));
      expect(await adapter.get('glyph:test')).toEqual({ a: 1 });
    });

    it('returns null when JSON.parse throws (corrupted data)', async () => {
      localStorage.setItem('glyph:bad', 'not-valid-json{{{');
      expect(await adapter.get<unknown>('glyph:bad')).toBeNull();
    });

    it('returns null for a null-valued item (impossible in localStorage, but defensive)', async () => {
      // localStorage.getItem returns null for missing keys — already tested above
      expect(await adapter.get('never-set')).toBeNull();
    });
  });

  // ─── set ─────────────────────────────────────────────────────────────────────

  describe('set', () => {
    it('serializes and stores the value', async () => {
      await adapter.set('glyph:items', [1, 2, 3]);
      expect(JSON.parse(localStorage.getItem('glyph:items')!)).toEqual([1, 2, 3]);
    });

    it('overwrites an existing key', async () => {
      await adapter.set('glyph:k', 'first');
      await adapter.set('glyph:k', 'second');
      expect(JSON.parse(localStorage.getItem('glyph:k')!)).toBe('second');
    });

    it('throws a friendly Error on QuotaExceededError', async () => {
      const quotaError = new DOMException('The quota has been exceeded.', 'QuotaExceededError');
      vi.spyOn(Storage.prototype, 'setItem').mockImplementationOnce(() => {
        throw quotaError;
      });
      await expect(adapter.set('k', 'v')).rejects.toThrow(/quota exceeded/i);
    });

    it('throws a friendly Error on NS_ERROR_DOM_QUOTA_REACHED', async () => {
      const quotaError = new DOMException('quota', 'NS_ERROR_DOM_QUOTA_REACHED');
      vi.spyOn(Storage.prototype, 'setItem').mockImplementationOnce(() => {
        throw quotaError;
      });
      await expect(adapter.set('k', 'v')).rejects.toThrow(/quota exceeded/i);
    });

    it('re-throws unrelated DOMExceptions unchanged', async () => {
      const securityError = new DOMException('security', 'SecurityError');
      vi.spyOn(Storage.prototype, 'setItem').mockImplementationOnce(() => {
        throw securityError;
      });
      await expect(adapter.set('k', 'v')).rejects.toThrow(securityError);
    });
  });

  // ─── remove ──────────────────────────────────────────────────────────────────

  describe('remove', () => {
    it('removes an existing key', async () => {
      localStorage.setItem('key', '"value"');
      await adapter.remove('key');
      expect(localStorage.getItem('key')).toBeNull();
    });

    it('does not throw when the key does not exist', async () => {
      await expect(adapter.remove('nonexistent')).resolves.toBeUndefined();
    });
  });

  // ─── keys ────────────────────────────────────────────────────────────────────

  describe('keys', () => {
    it('returns all keys currently in localStorage', async () => {
      localStorage.setItem('key1', '1');
      localStorage.setItem('key2', '2');
      const keys = await adapter.keys();
      expect(keys).toContain('key1');
      expect(keys).toContain('key2');
      expect(keys).toHaveLength(2);
    });

    it('returns an empty array when storage is empty', async () => {
      expect(await adapter.keys()).toEqual([]);
    });
  });

  // ─── clear ───────────────────────────────────────────────────────────────────

  describe('clear', () => {
    it('removes all keys from localStorage', async () => {
      localStorage.setItem('key1', '1');
      localStorage.setItem('key2', '2');
      await adapter.clear();
      expect(localStorage.length).toBe(0);
    });

    it('is a no-op when storage is already empty', async () => {
      await expect(adapter.clear()).resolves.toBeUndefined();
    });
  });

  // ─── localStorage undefined (SSR / non-browser) ──────────────────────────────

  describe('when typeof localStorage is undefined', () => {
    // Temporarily replace globalThis.localStorage with undefined to simulate SSR.
    const noop = async () => {};

    beforeEach(() => {
      vi.stubGlobal('localStorage', undefined);
    });

    afterEach(() => {
      vi.unstubAllGlobals();
    });

    it('get returns null without throwing', async () => {
      expect(await adapter.get('key')).toBeNull();
    });

    it('set returns undefined without throwing', async () => {
      await expect(adapter.set('key', 'value')).resolves.toBeUndefined();
    });

    it('remove returns undefined without throwing', async () => {
      await expect(adapter.remove('key')).resolves.toBeUndefined();
    });

    it('keys returns an empty array', async () => {
      expect(await adapter.keys()).toEqual([]);
    });

    it('clear returns undefined without throwing', async () => {
      await expect(adapter.clear()).resolves.toBeUndefined();
    });

    void noop; // suppress unused variable warning
  });
});
