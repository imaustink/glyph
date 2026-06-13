import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { now, makeTimestamps } from './time';

describe('now()', () => {
  it('returns a valid ISO string', () => {
    const result = now();
    expect(() => new Date(result)).not.toThrow();
    expect(new Date(result).toISOString()).toBe(result);
  });

  it('returns approximately the current time', () => {
    const before = Date.now();
    const result = now();
    const after = Date.now();
    const ts = new Date(result).getTime();
    expect(ts).toBeGreaterThanOrEqual(before);
    expect(ts).toBeLessThanOrEqual(after);
  });
});

describe('makeTimestamps()', () => {
  it('returns an object with createdAt and updatedAt', () => {
    const ts = makeTimestamps();
    expect(ts).toHaveProperty('createdAt');
    expect(ts).toHaveProperty('updatedAt');
  });

  it('createdAt and updatedAt are identical (same call, same ms)', () => {
    const ts = makeTimestamps();
    expect(ts.createdAt).toBe(ts.updatedAt);
  });

  it('both values are valid ISO strings', () => {
    const { createdAt, updatedAt } = makeTimestamps();
    expect(new Date(createdAt).toISOString()).toBe(createdAt);
    expect(new Date(updatedAt).toISOString()).toBe(updatedAt);
  });

  it('uses a consistent timestamp — both fields come from a single now() call', () => {
    // Run many times to verify consistency; if two separate now() calls were used,
    // a microsecond timing difference could (rarely) produce different values.
    for (let i = 0; i < 100; i++) {
      const ts = makeTimestamps();
      expect(ts.createdAt).toBe(ts.updatedAt);
    }
  });

  it('returns approximately the current time', () => {
    const before = Date.now();
    const { createdAt } = makeTimestamps();
    const after = Date.now();
    const ts = new Date(createdAt).getTime();
    expect(ts).toBeGreaterThanOrEqual(before);
    expect(ts).toBeLessThanOrEqual(after);
  });
});
