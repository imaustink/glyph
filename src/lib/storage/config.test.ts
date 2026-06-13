/**
 * Unit tests for storage/config.ts.
 *
 * config.ts reads VITE_STORAGE_MODE at module load time. The .env file sets
 * it to 'api' by default. We use vi.resetModules() + vi.stubEnv() to test
 * the local-storage branch (lines 54-55) and the api branch separately.
 */

import { describe, it, expect, vi, afterEach } from 'vitest';

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe('config — API mode (default from .env)', () => {
  it('exports a valid storageMode', async () => {
    const { storageMode } = await import('./config');
    expect(['local', 'api']).toContain(storageMode);
  });

  it('exports repositories with pages, tasks, lanes, and templates', async () => {
    const { repositories } = await import('./config');
    expect(repositories.pages).toBeDefined();
    expect(repositories.tasks).toBeDefined();
    expect(repositories.lanes).toBeDefined();
    expect(repositories.templates).toBeDefined();
  });
});

describe('config — local storage mode (covers lines 54-55)', () => {
  it('creates LocalStorageAdapter-backed repositories and sets orgs/shares to null', async () => {
    vi.stubEnv('VITE_STORAGE_MODE', 'local');
    vi.resetModules();

    const { repositories, storageMode } = await import('./config');

    expect(storageMode).toBe('local');
    // Local mode: orgs and shares are null (lines 54-62 executed)
    expect(repositories.orgs).toBeNull();
    expect(repositories.shares).toBeNull();
    // Core repos are still provided
    expect(repositories.pages).toBeDefined();
    expect(repositories.tasks).toBeDefined();
  });
});

describe('config — empty VITE_STORAGE_MODE falls back to local (covers || branch)', () => {
  it('defaults to local mode when VITE_STORAGE_MODE is empty string', async () => {
    vi.stubEnv('VITE_STORAGE_MODE', '');
    vi.resetModules();

    const { storageMode } = await import('./config');

    expect(storageMode).toBe('local');
  });
});
