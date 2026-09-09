/**
 * Shared constants for the Glyph app.
 *
 * Centralizes magic numbers, label mappings, and configuration values
 * to avoid duplication and make them easy to tune.
 */

import type { TaskStatus, Priority, TaskFilterField, FilterOperator } from './types';

// ─── Task Labels ──────────────────────────────────────────────────────────────

export const STATUS_LABELS: Record<TaskStatus, string> = {
	todo: 'Todo',
	'in-progress': 'In Progress',
	done: 'Done',
	cancelled: 'Cancelled'
};

/** Status options for select inputs, in workflow order. */
export const STATUS_OPTIONS: { value: TaskStatus; label: string }[] = [
	{ value: 'todo', label: 'Todo' },
	{ value: 'in-progress', label: 'In Progress' },
	{ value: 'done', label: 'Done' },
	{ value: 'cancelled', label: 'Cancelled' }
];

export const PRIORITY_LABELS: Record<Priority, string> = {
	urgent: 'Urgent',
	high: 'High',
	medium: 'Medium',
	low: 'Low',
	none: ''
};

/**
 * Priority options for select inputs, ordered from lowest to highest urgency.
 * Unlike PRIORITY_LABELS, 'none' has a visible label here.
 */
export const PRIORITY_OPTIONS: { value: Priority; label: string }[] = [
	{ value: 'none', label: 'None' },
	{ value: 'low', label: 'Low' },
	{ value: 'medium', label: 'Medium' },
	{ value: 'high', label: 'High' },
	{ value: 'urgent', label: 'Urgent' }
];

/**
 * Sort weight for priorities — lower weight sorts first (higher urgency).
 * Shared by sort providers so task and note priority order consistently.
 */
export const PRIORITY_WEIGHT: Record<Priority, number> = {
	urgent: 0,
	high: 1,
	medium: 2,
	low: 3,
	none: 4
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

// ─── Filter Field Metadata ────────────────────────────────────────────────────

/**
 * The kind of value a filterable field holds, driving which input control the
 * filter builder renders and which operators are valid for it.
 * - `enum`: fixed set of string values (e.g. status, priority) — renders a select.
 * - `text`: free-form string — renders a text input.
 * - `date`: ISO date string — supports before/after comparisons.
 * - `tags`: string array membership — renders a tag input/autocomplete.
 * - `note`: references a page/note by id — renders a note picker.
 */
export type FilterFieldKind = 'enum' | 'text' | 'date' | 'tags' | 'note';

export interface FilterFieldMeta {
	kind: FilterFieldKind;
	label: string;
	/** Operators that are meaningful for this field's kind. */
	operators: FilterOperator[];
	/** Valid values for `enum` fields, used to render a select instead of free text. */
	options?: { value: string; label: string }[];
}

const ENUM_OPERATORS: FilterOperator[] = ['any', 'eq', 'neq', 'in', 'not_in', 'exists', 'not_exists'];
const TEXT_OPERATORS: FilterOperator[] = ['any', 'eq', 'neq', 'contains', 'exists', 'not_exists'];
const DATE_OPERATORS: FilterOperator[] = ['any', 'eq', 'neq', 'before', 'after', 'exists', 'not_exists'];
const TAGS_OPERATORS: FilterOperator[] = ['any', 'contains', 'in', 'not_in', 'exists', 'not_exists'];
const NOTE_OPERATORS: FilterOperator[] = ['any', 'eq', 'neq', 'in', 'not_in', 'exists', 'not_exists'];

/**
 * Per-field metadata for the task filter builder (`LaneConfig.svelte`). Keeps the
 * UI type-aware: enum fields like status/priority can only ever hold a value that
 * actually exists on the type, instead of an arbitrary free-text string.
 */
export const FILTER_FIELD_META: Record<TaskFilterField, FilterFieldMeta> = {
	status: { kind: 'enum', label: 'Status', operators: ENUM_OPERATORS, options: STATUS_OPTIONS },
	priority: { kind: 'enum', label: 'Priority', operators: ENUM_OPERATORS, options: PRIORITY_OPTIONS },
	dueDate: { kind: 'date', label: 'Due Date', operators: DATE_OPERATORS },
	createdAt: { kind: 'date', label: 'Created', operators: DATE_OPERATORS },
	updatedAt: { kind: 'date', label: 'Updated', operators: DATE_OPERATORS },
	title: { kind: 'text', label: 'Title', operators: TEXT_OPERATORS },
	tags: { kind: 'tags', label: 'Tags', operators: TAGS_OPERATORS },
	sourcePageId: { kind: 'note', label: 'Source Note', operators: NOTE_OPERATORS },
	sourcePageTags: { kind: 'tags', label: 'Source Note Tag', operators: TAGS_OPERATORS }
};
