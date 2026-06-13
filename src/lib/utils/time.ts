/**
 * Time utilities for consistent timestamp handling.
 */

/**
 * Returns the current time as an ISO string.
 * Used for createdAt/updatedAt timestamps in stores.
 *
 * In API mode these client-generated timestamps are used only for optimistic
 * UI updates — the Go API always overwrites updated_at with `NOW()` in SQL,
 * so the server clock is the authoritative source of truth for persistence.
 * In localStorage mode this is the only clock (acceptable for single-device use).
 */
export function now(): string {
	return new Date().toISOString();
}

/**
 * Returns a { createdAt, updatedAt } pair sharing a single timestamp.
 * Always use this instead of two separate now() calls so both fields
 * are guaranteed to be identical at creation time.
 */
export function makeTimestamps(): { createdAt: string; updatedAt: string } {
	const ts = now();
	return { createdAt: ts, updatedAt: ts };
}
