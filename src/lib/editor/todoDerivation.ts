/**
 * How a note's content maps to tasks: which bullets are TODO bullets, and
 * how a task's status shows on its bullet.
 *
 * The Go API does the same derivation for content written outside the
 * editor (api/internal/pmmd/todo.go, used by MCP). Both are tested against
 * one shared fixture file, api/internal/pmmd/testdata/todo_derivation.json,
 * so they can't drift apart. Change them together.
 *
 * Keep this module free of DOM, Svelte and store imports.
 */
import type { TaskStatus } from '$lib/models/types';

/** A bullet shows as ticked when its task is finished either way. */
export function isCheckedStatus(status: TaskStatus | string): boolean {
	return status === 'done' || status === 'cancelled';
}
