/**
 * Shared constants for the Glyph app.
 *
 * Centralizes magic numbers, label mappings, and configuration values
 * to avoid duplication and make them easy to tune.
 */

import type { TaskStatus, Priority } from './types';

// ─── Task Labels ──────────────────────────────────────────────────────────────

export const STATUS_LABELS: Record<TaskStatus, string> = {
	todo: 'Todo',
	'in-progress': 'In Progress',
	done: 'Done',
	cancelled: 'Cancelled'
};

export const PRIORITY_LABELS: Record<Priority, string> = {
	urgent: 'Urgent',
	high: 'High',
	medium: 'Medium',
	low: 'Low',
	none: ''
};

/**
 * Status cycle for quick task progression.
 * Clicking the status badge cycles through: todo → in-progress → done → todo
 * Cancelled tasks reset to todo.
 */
export const STATUS_CYCLE: Record<TaskStatus, TaskStatus> = {
	todo: 'in-progress',
	'in-progress': 'done',
	done: 'todo',
	cancelled: 'todo'
};

// ─── Debounce Timings (ms) ────────────────────────────────────────────────────

export const DEBOUNCE = {
	/** Content save debounce in editor */
	CONTENT_SAVE: 500,
	/** Task title inline edit debounce */
	TASK_TITLE: 500,
	/** Description field debounce on task detail */
	DESCRIPTION: 600,
	/** Hover preview delay before showing */
	HOVER_PREVIEW: 300,
	/** Search input debounce */
	SEARCH: 150,
	/** Dropdown blur timeout for tag input */
	DROPDOWN_BLUR: 150
} as const;

// ─── UI Limits ────────────────────────────────────────────────────────────────

export const LIMITS = {
	/** Max tags to show on task card before truncating */
	MAX_VISIBLE_TAGS: 3,
	/** Max suggestions in autocomplete dropdowns */
	MAX_SUGGESTIONS: 8
} as const;

// ─── Enum Validation ──────────────────────────────────────────────────────────

const VALID_STATUSES = new Set<TaskStatus>(['todo', 'in-progress', 'done', 'cancelled']);
const VALID_PRIORITIES = new Set<Priority>(['urgent', 'high', 'medium', 'low', 'none']);

/**
 * Validate and parse a task status value.
 * Returns the status if valid, otherwise returns the fallback (default: 'todo').
 */
export function parseTaskStatus(value: unknown, fallback: TaskStatus = 'todo'): TaskStatus {
	if (typeof value === 'string' && VALID_STATUSES.has(value as TaskStatus)) {
		return value as TaskStatus;
	}
	return fallback;
}

/**
 * Validate and parse a priority value.
 * Returns the priority if valid, otherwise returns the fallback (default: 'none').
 */
export function parsePriority(value: unknown, fallback: Priority = 'none'): Priority {
	if (typeof value === 'string' && VALID_PRIORITIES.has(value as Priority)) {
		return value as Priority;
	}
	return fallback;
}

/**
 * Check if a value is a valid task status.
 */
export function isValidTaskStatus(value: unknown): value is TaskStatus {
	return typeof value === 'string' && VALID_STATUSES.has(value as TaskStatus);
}

/**
 * Check if a value is a valid priority.
 */
export function isValidPriority(value: unknown): value is Priority {
	return typeof value === 'string' && VALID_PRIORITIES.has(value as Priority);
}
