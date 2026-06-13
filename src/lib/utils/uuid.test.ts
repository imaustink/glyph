/**
 * Unit tests for the uuid() utility.
 *
 * Covers:
 * - crypto.randomUUID path (default)
 * - Math.random fallback when crypto.randomUUID is unavailable
 * - Output format conformance (RFC 4122 v4)
 */

import { describe, it, expect, vi, afterEach } from 'vitest';
import { uuid } from './uuid';

const UUID_V4_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

describe('uuid', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('crypto.randomUUID path', () => {
    it('returns a valid v4 UUID', () => {
      const result = uuid();
      expect(result).toMatch(UUID_V4_RE);
    });

    it('returns a different UUID on each call', () => {
      const a = uuid();
      const b = uuid();
      expect(a).not.toBe(b);
    });

    it('delegates to crypto.randomUUID when available', () => {
      const spy = vi.spyOn(crypto, 'randomUUID').mockReturnValue(
        '12345678-1234-4123-a123-123456789abc' as ReturnType<typeof crypto.randomUUID>
      );
      const result = uuid();
      expect(spy).toHaveBeenCalledOnce();
      expect(result).toBe('12345678-1234-4123-a123-123456789abc');
    });
  });

  describe('Math.random fallback path', () => {
    it('returns a valid v4 UUID when crypto.randomUUID is unavailable', () => {
      // Replace the global crypto with an object that has no randomUUID function.
      // This makes the `typeof crypto.randomUUID === 'function'` check fail.
      const originalCrypto = globalThis.crypto;
      vi.stubGlobal('crypto', {
        ...originalCrypto,
        randomUUID: undefined
      });

      try {
        const result = uuid();
        expect(result).toMatch(UUID_V4_RE);
      } finally {
        vi.unstubAllGlobals();
      }
    });

    it('returns a different UUID on each call via fallback', () => {
      const originalCrypto = globalThis.crypto;
      vi.stubGlobal('crypto', { ...originalCrypto, randomUUID: undefined });

      try {
        const a = uuid();
        const b = uuid();
        expect(a).not.toBe(b);
      } finally {
        vi.unstubAllGlobals();
      }
    });

    it('fallback UUID has version 4 in the correct nibble position', () => {
      const originalCrypto = globalThis.crypto;
      vi.stubGlobal('crypto', { ...originalCrypto, randomUUID: undefined });

      try {
        const result = uuid();
        // 15th character (index 14) must be '4'
        expect(result[14]).toBe('4');
        // 20th character (index 19) must be 8, 9, a, or b
        expect(result[19]).toMatch(/[89ab]/i);
      } finally {
        vi.unstubAllGlobals();
      }
    });
  });
});
