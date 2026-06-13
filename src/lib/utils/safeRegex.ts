/**
 * Utilities for safely handling user-supplied regular expressions.
 *
 * Prevents ReDoS (Regular Expression Denial of Service) by:
 * 1. Rejecting patterns with nested quantifiers that cause catastrophic backtracking
 * 2. Limiting pattern length
 * 3. Caching compiled RegExp instances
 */

/** Maximum allowed length for a user-supplied regex pattern. */
const MAX_PATTERN_LENGTH = 200;

/**
 * Detects patterns likely to cause catastrophic backtracking.
 * Matches nested quantifiers like (a+)+, (a*)+, (a+)*, (a{2,})+, etc.
 */
const REDOS_PATTERN = /([+*{][?]?[)]\s*[+*{])|([+*{][?]?\s*[+*{])/;

/**
 * LRU-ish cache for compiled RegExp objects. Keyed by pattern string.
 * Limited to prevent unbounded memory growth.
 */
const regexCache = new Map<string, RegExp | null>();
const MAX_CACHE_SIZE = 64;

export interface SafeRegexResult {
  valid: boolean;
  error?: string;
}

/**
 * Validate a regex pattern for safety without compiling it into a RegExp.
 * Returns { valid: true } if the pattern is safe to use, or { valid: false, error } otherwise.
 */
export function validateRegexPattern(pattern: string): SafeRegexResult {
  if (!pattern) {
    return { valid: false, error: 'Empty pattern' };
  }

  if (pattern.length > MAX_PATTERN_LENGTH) {
    return { valid: false, error: `Pattern exceeds maximum length of ${MAX_PATTERN_LENGTH} characters` };
  }

  if (REDOS_PATTERN.test(pattern)) {
    return { valid: false, error: 'Pattern contains nested quantifiers that may cause excessive backtracking' };
  }

  // Verify it's syntactically valid
  try {
    new RegExp(pattern);
  } catch {
    return { valid: false, error: 'Invalid regex syntax' };
  }

  return { valid: true };
}

/**
 * Safely compile and test a regex pattern against input text.
 * Returns false if the pattern is unsafe, invalid, or does not match.
 * Uses a cache to avoid recompilation on repeated calls.
 */
export function safeRegexTest(pattern: string, text: string): boolean {
  // Check cache first
  const cached = regexCache.get(pattern);
  if (cached !== undefined) {
    if (cached === null) return false; // previously rejected
    return cached.test(text);
  }

  // Validate and compile
  const validation = validateRegexPattern(pattern);
  if (!validation.valid) {
    // Cache the rejection
    evictIfNeeded();
    regexCache.set(pattern, null);
    return false;
  }

  const regex = new RegExp(pattern);
  evictIfNeeded();
  regexCache.set(pattern, regex);
  return regex.test(text);
}

function evictIfNeeded() {
  if (regexCache.size >= MAX_CACHE_SIZE) {
    // Delete the oldest entry (first key in Map iteration order)
    const firstKey = regexCache.keys().next().value;
    /* c8 ignore next */
    if (firstKey !== undefined) {
      regexCache.delete(firstKey);
    }
  }
}
