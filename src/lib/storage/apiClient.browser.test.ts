/**
 * Tests for apiClient with browser=true.
 *
 * This file is isolated from apiClient.test.ts so each file gets its own
 * module instance (and thus its own `_redirecting = false` state).
 *
 * Covers the redirect logic in handleAuthError (lines 45-54) and the
 * error-body parsing catch paths (lines 114-115).
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Must mock before import so the module picks up browser: true
vi.mock('$app/environment', () => ({ browser: true }));
vi.mock('$lib/stores/ui.svelte', () => ({
  uiStore: { waitForSaveComplete: vi.fn().mockResolvedValue(undefined) }
}));

import { handleAuthError, UnauthorizedError, ApiError, api } from './apiClient';

describe('handleAuthError (browser=true)', () => {
  let assignMock: ReturnType<typeof vi.fn>;
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    // jsdom's window.location is non-configurable, so we use vi.stubGlobal
    assignMock = vi.fn();
    vi.stubGlobal('location', {
      assign: assignMock,
      pathname: '/current',
      search: '',
      origin: 'http://localhost'
    });
    fetchSpy = vi.spyOn(globalThis, 'fetch');
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('throws synchronously and schedules a redirect via location.assign (covers lines 45-54)', async () => {
    const err = new UnauthorizedError('GET', '/api/v1/x');

    // First call: _redirecting=false → sets true, schedules redirect, throws
    expect(() => handleAuthError(err)).toThrow(err);

    // Second call: _redirecting=true → redirect suppressed, still throws
    // (covers the if (!_redirecting) false branch)
    expect(() => handleAuthError(err)).toThrow(err);

    // Allow dynamic import + waitForSaveComplete.finally() to resolve
    await new Promise((r) => setTimeout(r, 50));
    expect(assignMock).toHaveBeenCalledWith(expect.stringContaining('/auth/login'));
  });

  // ─── Error body parsing catch paths (lines 114-115) ────────────────────

  it('handles json() failure gracefully for non-ok JSON responses', async () => {
    fetchSpy.mockResolvedValueOnce({
      status: 500,
      ok: false,
      headers: { get: (k: string) => (k === 'content-type' ? 'application/json' : null) },
      json: async () => { throw new Error('bad json'); }
    } as unknown as Response);
    await expect(api.get('/api/v1/fail')).rejects.toBeInstanceOf(ApiError);
  });

  it('handles text() failure gracefully for non-ok text responses', async () => {
    fetchSpy.mockResolvedValueOnce({
      status: 503,
      ok: false,
      headers: { get: () => null },
      text: async () => { throw new Error('bad text'); }
    } as unknown as Response);
    await expect(api.get('/api/v1/fail')).rejects.toBeInstanceOf(ApiError);
  });
});
