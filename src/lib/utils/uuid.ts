/**
 * Generate a UUID v4 string.
 *
 * Uses crypto.randomUUID() when available (secure contexts).
 * Falls back to Math.random-based generation for non-secure contexts
 * (e.g., HTTP without localhost during development).
 */
export function uuid(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  // Fallback: RFC 4122 version 4 UUID using Math.random
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
  });
}
