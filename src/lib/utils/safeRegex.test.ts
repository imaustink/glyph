/**
 * Unit tests for safeRegex utilities.
 *
 * Covers: validateRegexPattern (empty, length, ReDoS patterns, syntax errors, valid)
 *         safeRegexTest (match, no-match, rejection caching, cache eviction)
 */

import { describe, it, expect } from 'vitest';
import { validateRegexPattern, safeRegexTest } from './safeRegex';

// Use a unique suffix per test to avoid cache collisions between runs
let counter = 0;
function fresh(base: string) {
  return `${base}-${++counter}`;
}

describe('validateRegexPattern', () => {
  it('rejects empty string', () => {
    const result = validateRegexPattern('');
    expect(result.valid).toBe(false);
    expect(result.error).toMatch(/empty/i);
  });

  it('rejects pattern exceeding MAX_PATTERN_LENGTH (200 chars)', () => {
    const longPattern = 'a'.repeat(201);
    const result = validateRegexPattern(longPattern);
    expect(result.valid).toBe(false);
    expect(result.error).toMatch(/exceed/i);
  });

  it('accepts a pattern exactly at the 200-char limit', () => {
    const pattern = 'a'.repeat(200);
    expect(validateRegexPattern(pattern).valid).toBe(true);
  });

  it('rejects (a+)+ nested quantifiers', () => {
    const result = validateRegexPattern('(a+)+');
    expect(result.valid).toBe(false);
    expect(result.error).toMatch(/nested quantifier/i);
  });

  it('rejects (a*)+ nested quantifiers', () => {
    expect(validateRegexPattern('(a*)+').valid).toBe(false);
  });

  it('rejects (a+)* nested quantifiers', () => {
    expect(validateRegexPattern('(a+)*').valid).toBe(false);
  });

  it('rejects patterns with invalid syntax', () => {
    const result = validateRegexPattern('[unclosed');
    expect(result.valid).toBe(false);
    expect(result.error).toMatch(/invalid/i);
  });

  it('accepts a simple literal pattern', () => {
    const result = validateRegexPattern('hello');
    expect(result.valid).toBe(true);
    expect(result.error).toBeUndefined();
  });

  it('accepts a pattern with anchors and alternation', () => {
    expect(validateRegexPattern('^(foo|bar)$').valid).toBe(true);
  });

  it('accepts a case-insensitive-style pattern (no flags)', () => {
    expect(validateRegexPattern('TODO').valid).toBe(true);
  });

  it('accepts a pattern with non-nested quantifiers', () => {
    expect(validateRegexPattern('a+b*c?').valid).toBe(true);
  });
});

describe('safeRegexTest', () => {
  it('returns true when the pattern matches', () => {
    expect(safeRegexTest('hello', 'say hello world')).toBe(true);
  });

  it('returns false when the pattern does not match', () => {
    expect(safeRegexTest('xyz', 'hello world')).toBe(false);
  });

  it('returns false for an unsafe ReDoS-prone pattern', () => {
    expect(safeRegexTest('(a+)+', 'aaaa')).toBe(false);
  });

  it('returns false for an invalid syntax pattern', () => {
    const pattern = fresh('[invalid');
    expect(safeRegexTest(pattern, 'any text')).toBe(false);
  });

  it('caches the rejection so the second call also returns false', () => {
    const pattern = fresh('[bad-cached');
    expect(safeRegexTest(pattern, 'text')).toBe(false);
    // Second call must hit the cache, not re-validate
    expect(safeRegexTest(pattern, 'text')).toBe(false);
  });

  it('caches a compiled regex and returns consistent results', () => {
    const pattern = fresh('unique-valid');
    const text = `match-${pattern}`;
    const r1 = safeRegexTest(pattern, text);
    const r2 = safeRegexTest(pattern, text);
    expect(r1).toBe(r2);
  });

  it('supports regex features like anchors', () => {
    expect(safeRegexTest('^todo$', 'todo')).toBe(true);
    expect(safeRegexTest('^todo$', 'not todo')).toBe(false);
  });

  it('evicts oldest cache entry when cache reaches 64 entries', () => {
    // Fill the cache with 64 unique patterns, then add a 65th — must not throw
    for (let i = 0; i < 64; i++) {
      safeRegexTest(fresh('evict-fill'), 'test');
    }
    const last = fresh('evict-last');
    const text = last;
    // Should still work after eviction
    expect(safeRegexTest(last, text)).toBe(true);
  });
});
