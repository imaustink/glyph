/**
 * Covers the .catch() callback in handleAuthError's redirect chain.
 * This file gets its own module instance (fresh _redirecting=false state).
 * We make waitForSaveComplete reject so the .catch(() => {}) path executes.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('$app/environment', () => ({ browser: true }));
vi.mock('$lib/stores/ui.svelte', () => ({
  uiStore: { waitForSaveComplete: vi.fn().mockRejectedValue(new Error('timeout')) }
}));

import { handleAuthError, UnauthorizedError } from './apiClient';

describe('handleAuthError redirect .catch() callback (covers the () => {} catch on line 53)', () => {
  beforeEach(() => {
    vi.stubGlobal('location', {
      assign: vi.fn(),
      pathname: '/page',
      search: '',
      origin: 'http://localhost'
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('silently swallows the rejection and still redirects via .finally() (covers the .catch body)', async () => {
    const err = new UnauthorizedError('GET', '/api/v1/x');

    // _redirecting is false in this fresh module → enters the redirect block
    expect(() => handleAuthError(err)).toThrow(err);

    // Allow the import().then().catch().finally() chain to complete
    await new Promise((r) => setTimeout(r, 50));

    // The .catch(() => {}) swallowed the rejection from waitForSaveComplete,
    // and .finally() still runs location.assign
    expect((location.assign as ReturnType<typeof vi.fn>)).toHaveBeenCalledWith(
      expect.stringContaining('/auth/login')
    );
  });
});
