/**
 * Unit tests for the apiClient `request` function (via the `api` export).
 *
 * Uses vi.spyOn(globalThis, 'fetch') — no real network calls are made.
 * Covers: 204 no-content, 401 → UnauthorizedError, non-ok → ApiError,
 *         AbortError → TimeoutError, generic fetch throw, JSON response,
 *         error body parsing (JSON + text), credentials/headers, body serialization,
 *         handleAuthError (all branches), api.get/post/patch/put/del.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// apiClient.ts imports `browser` from '$app/environment' — mock it as false
// so handleAuthError re-throws without trying to redirect (no location mock needed).
vi.mock('$app/environment', () => ({ browser: false }));

// Static import — same module instance for all tests, proper V8 coverage.
import { api, handleAuthError, ApiError, UnauthorizedError, TimeoutError, API_BASE } from './apiClient';

describe('apiClient', () => {
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    fetchSpy = vi.spyOn(globalThis, 'fetch');
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function mockFetch(
    status: number,
    body: unknown,
    contentType = 'application/json'
  ) {
    fetchSpy.mockResolvedValueOnce({
      status,
      ok: status >= 200 && status < 300,
      headers: {
        get: (key: string) => (key === 'content-type' ? contentType : null)
      },
      json: async () => body,
      text: async () => (typeof body === 'string' ? body : JSON.stringify(body))
    } as unknown as Response);
  }

  // ─── Successful responses ────────────────────────────────────────────────

  describe('successful 2xx responses', () => {
    it('returns parsed JSON for a 200 response', async () => {
      mockFetch(200, { id: 'user-1' });
      const result = await api.get<{ id: string }>('/api/v1/me');
      expect(result).toEqual({ id: 'user-1' });
    });

    it('returns undefined for a 204 No Content response', async () => {
      fetchSpy.mockResolvedValueOnce({ status: 204, ok: true, headers: { get: () => null } } as unknown as Response);
      const result = await api.del('/api/v1/resource/1');
      expect(result).toBeUndefined();
    });

    it('sends POST body as JSON', async () => {
      mockFetch(201, { id: 'new' });
      await api.post('/api/v1/items', { title: 'test' });
      const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
      expect(init?.body).toBe(JSON.stringify({ title: 'test' }));
    });

    it('sends PATCH body as JSON', async () => {
      mockFetch(200, {});
      await api.patch('/api/v1/items/1', { title: 'updated' });
      const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
      expect(init?.body).toBe(JSON.stringify({ title: 'updated' }));
    });

    it('sends PUT body as JSON', async () => {
      mockFetch(200, {});
      await api.put('/api/v1/items/1', { content: 'hello' });
      const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
      expect(init?.method).toBe('PUT');
      expect(init?.body).toBe(JSON.stringify({ content: 'hello' }));
    });

    it('sends GET without a body', async () => {
      mockFetch(200, []);
      await api.get('/api/v1/items');
      const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
      expect(init?.body).toBeUndefined();
    });
  });

  // ─── Request headers and credentials ────────────────────────────────────

  describe('request headers and credentials', () => {
    it('includes credentials: include on every request', async () => {
      mockFetch(200, {});
      await api.get('/api/v1/x');
      const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
      expect(init?.credentials).toBe('include');
    });

    it('includes Content-Type: application/json', async () => {
      mockFetch(200, {});
      await api.get('/api/v1/x');
      const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
      expect((init?.headers as Record<string, string>)?.['Content-Type']).toBe('application/json');
    });

    it('includes X-Requested-With: XMLHttpRequest', async () => {
      mockFetch(200, {});
      await api.get('/api/v1/x');
      const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit];
      expect((init?.headers as Record<string, string>)?.['X-Requested-With']).toBe('XMLHttpRequest');
    });

    it('prepends API_BASE to the path', async () => {
      mockFetch(200, {});
      await api.get('/api/v1/test');
      const [url] = fetchSpy.mock.calls[0] as [string, RequestInit];
      expect(url).toBe(`${API_BASE}/api/v1/test`);
    });
  });

  // ─── Error responses ─────────────────────────────────────────────────────

  describe('401 → UnauthorizedError', () => {
    it('throws UnauthorizedError for a 401 response', async () => {
      fetchSpy.mockResolvedValueOnce({ status: 401, ok: false, headers: { get: () => null } } as unknown as Response);
      await expect(api.get('/api/v1/me')).rejects.toBeInstanceOf(UnauthorizedError);
    });

    it('UnauthorizedError carries status 401 and path', async () => {
      fetchSpy.mockResolvedValueOnce({ status: 401, ok: false, headers: { get: () => null } } as unknown as Response);
      const err = await api.get('/api/v1/protected').catch((e) => e);
      expect(err).toBeInstanceOf(UnauthorizedError);
      expect((err as InstanceType<typeof UnauthorizedError>).status).toBe(401);
      expect((err as InstanceType<typeof UnauthorizedError>).path).toBe('/api/v1/protected');
    });
  });

  describe('non-ok status → ApiError', () => {
    it('throws ApiError for a 500 response with JSON body', async () => {
      mockFetch(500, { error: 'internal error' }, 'application/json');
      await expect(api.get('/api/v1/fail')).rejects.toBeInstanceOf(ApiError);
    });

    it('throws ApiError for a 422 response with text body', async () => {
      mockFetch(422, 'validation failed', 'text/plain');
      const err = await api.get('/api/v1/validate').catch((e) => e);
      expect(err).toBeInstanceOf(ApiError);
      expect((err as InstanceType<typeof ApiError>).status).toBe(422);
      expect((err as InstanceType<typeof ApiError>).body).toBe('validation failed');
    });

    it('ApiError message includes method and path', async () => {
      fetchSpy.mockResolvedValueOnce({
        status: 404,
        ok: false,
        headers: { get: () => null },
        text: async () => ''
      } as unknown as Response);
      const err = await api.get('/api/v1/missing').catch((e: unknown) => e);
      expect((err as ApiError).message).toContain('GET');
      expect((err as ApiError).message).toContain('/api/v1/missing');
    });

    it('uses empty string body when res.text() rejects (covers () => \'\' catch callback)', async () => {
      fetchSpy.mockResolvedValueOnce({
        status: 503,
        ok: false,
        headers: { get: (key: string) => (key === 'content-type' ? 'text/plain' : null) },
        text: async () => { throw new Error('text parse error'); }
      } as unknown as Response);
      const err = await api.get('/api/v1/fail-text').catch((e) => e);
      expect(err).toBeInstanceOf(ApiError);
      expect((err as InstanceType<typeof ApiError>).body).toBe('');
    });
  });

  describe('TimeoutError', () => {
    it('throws TimeoutError when fetch throws an AbortError', async () => {
      fetchSpy.mockRejectedValueOnce(new DOMException('The operation was aborted.', 'AbortError'));
      await expect(api.get('/api/v1/slow')).rejects.toBeInstanceOf(TimeoutError);
    });

    it('TimeoutError message includes the path', async () => {
      fetchSpy.mockRejectedValueOnce(new DOMException('aborted', 'AbortError'));
      const err = await api.get('/api/v1/slow').catch((e: unknown) => e);
      expect((err as TimeoutError).message).toContain('/api/v1/slow');
    });

    it('aborts via setTimeout after REQUEST_TIMEOUT_MS (covers () => controller.abort() at line 78)', async () => {
      vi.useFakeTimers();
      try {
        // Make fetch hang until the AbortSignal fires
        fetchSpy.mockImplementationOnce((_url: string, init?: RequestInit) =>
          new Promise<Response>((_, reject) => {
            (init?.signal as AbortSignal).addEventListener('abort', () =>
              reject(new DOMException('The operation was aborted.', 'AbortError'))
            );
          })
        );

        const requestPromise = api.get('/api/v1/slow-real');
        // Advance timers by 15 seconds to trigger the setTimeout callback
        vi.advanceTimersByTime(15_000);
        await expect(requestPromise).rejects.toBeInstanceOf(TimeoutError);
      } finally {
        vi.useRealTimers();
      }
    });
  });

  describe('network errors (non-abort)', () => {
    it('re-throws non-AbortError fetch failures as-is', async () => {
      const netError = new TypeError('Failed to fetch');
      fetchSpy.mockRejectedValueOnce(netError);
      await expect(api.get('/api/v1/x')).rejects.toBe(netError);
    });
  });

  // ─── handleAuthError ─────────────────────────────────────────────────────

  describe('handleAuthError', () => {
    it('re-throws when err is not an UnauthorizedError', () => {
      const err = new Error('generic error');
      expect(() => handleAuthError(err)).toThrow(err);
    });

    it('re-throws UnauthorizedError when browser is false', () => {
      // Our mock has browser=false, so the redirect block is skipped
      const err = new UnauthorizedError('GET', '/api/v1/x');
      expect(() => handleAuthError(err)).toThrow(err);
    });

    it('re-throws UnauthorizedError with a plain Error wrapping', () => {
      const err = new UnauthorizedError('POST', '/api/v1/items');
      let thrown: unknown;
      try { handleAuthError(err); } catch (e) { thrown = e; }
      expect(thrown).toBe(err);
    });

    it('re-throws ApiError when it is not UnauthorizedError', () => {
      const err = new ApiError(500, 'GET', '/api/v1/x', null);
      expect(() => handleAuthError(err)).toThrow(err);
    });
  });

  // ─── Error classes ────────────────────────────────────────────────────────

  describe('ApiError', () => {
    it('has name ApiError', () => {
      expect(new ApiError(404, 'GET', '/path', null).name).toBe('ApiError');
    });

    it('is an instance of Error', () => {
      expect(new ApiError(500, 'GET', '/x', null)).toBeInstanceOf(Error);
    });

    it('stores status, method, path, body', () => {
      const err = new ApiError(403, 'POST', '/api/v1/things', { reason: 'forbidden' });
      expect(err.status).toBe(403);
      expect(err.method).toBe('POST');
      expect(err.path).toBe('/api/v1/things');
      expect(err.body).toEqual({ reason: 'forbidden' });
    });
  });

  describe('UnauthorizedError', () => {
    it('is an instance of ApiError', () => {
      expect(new UnauthorizedError('GET', '/x')).toBeInstanceOf(ApiError);
    });

    it('has status 401', () => {
      expect(new UnauthorizedError('GET', '/x').status).toBe(401);
    });

    it('has name UnauthorizedError', () => {
      expect(new UnauthorizedError('GET', '/x').name).toBe('UnauthorizedError');
    });
  });

  describe('TimeoutError', () => {
    it('has name TimeoutError and is an instance of Error', () => {
      const err = new TimeoutError('GET', '/x');
      expect(err.name).toBe('TimeoutError');
      expect(err).toBeInstanceOf(Error);
    });

    it('message includes method, path, and timeout', () => {
      const err = new TimeoutError('DELETE', '/api/v1/resource');
      expect(err.message).toContain('DELETE');
      expect(err.message).toContain('/api/v1/resource');
      expect(err.message).toContain('ms');
    });
  });

  // ─── api.getOrNull ───────────────────────────────────────────────────────

  describe('api.getOrNull', () => {
    it('returns parsed data on success', async () => {
      mockFetch(200, { id: 'user-1' });
      const result = await api.getOrNull<{ id: string }>('/api/v1/me');
      expect(result).toEqual({ id: 'user-1' });
    });

    it('returns null when a non-401 error occurs', async () => {
      mockFetch(500, { error: 'internal server error' });
      const result = await api.getOrNull('/api/v1/me');
      expect(result).toBeNull();
    });

    it('re-throws UnauthorizedError (does not swallow 401)', async () => {
      mockFetch(401, { error: 'unauthorized' });
      await expect(api.getOrNull('/api/v1/me')).rejects.toBeInstanceOf(UnauthorizedError);
    });
  });
});
