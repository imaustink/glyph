/**
 * Shared task-filter logic used by both the localStorage (TaskRepository)
 * and API-backed (ApiTaskRepository) repositories.
 *
 * Single source of truth — fixes the prior situation where identical logic
 * was copy-pasted into two files and had already silently diverged (the
 * `before`/`after` operators handled the date-field guard differently).
 */

import type { Task, FilterSet, FilterRule, FilterValue } from '$lib/models/types';

/** Fields on Task that hold ISO date strings and may be compared with before/after operators. */
const DATE_FIELDS: ReadonlySet<string> = new Set(['dueDate', 'createdAt', 'updatedAt']);

/**
 * Context that resolves synthetic (computed) filter fields which are not direct
 * properties of a Task. Supplied by callers that have access to related data
 * (e.g. the page tree). Optional — when absent, synthetic fields resolve to
 * empty values so their rules simply match nothing.
 */
export interface FilterContext {
  /** Return the tags of the note (source page) a task was created from. */
  getSourcePageTags?: (task: Task) => string[];
}

/** Case-insensitive membership test for a tag within a tag list. */
function hasTag(tags: string[], value: FilterValue): boolean {
  const needle = String(value ?? '').toLowerCase();
  return tags.some((t) => t.toLowerCase() === needle);
}

/** True when the tag list intersects any of the provided values. */
function intersectsTags(tags: string[], value: FilterValue): boolean {
  const values = Array.isArray(value) ? value : [value];
  const lowered = new Set(tags.map((t) => t.toLowerCase()));
  return values.some((v) => lowered.has(String(v ?? '').toLowerCase()));
}

/** Evaluate a rule against an array of tags (used for the synthetic sourcePageTags field). */
function matchesTagRule(tags: string[], rule: FilterRule): boolean {
  switch (rule.operator) {
    case 'any':
      return true;
    case 'contains':
    case 'eq':
      return hasTag(tags, rule.value);
    case 'neq':
      return !hasTag(tags, rule.value);
    case 'in':
      return intersectsTags(tags, rule.value);
    case 'not_in':
      return !intersectsTags(tags, rule.value);
    case 'exists':
      return tags.length > 0;
    case 'not_exists':
      return tags.length === 0;
    default:
      return true;
  }
}

export function matchesRule(task: Task, rule: FilterRule, ctx?: FilterContext): boolean {
  // Synthetic field: tags of the task's source note.
  if (rule.field === 'sourcePageTags') {
    const tags = ctx?.getSourcePageTags?.(task) ?? [];
    return matchesTagRule(tags, rule);
  }

  const raw = task[rule.field];

  switch (rule.operator) {
    case 'eq':
      return raw === rule.value;
    case 'neq':
      return raw !== rule.value;
    case 'in':
      return Array.isArray(rule.value) && (rule.value as FilterValue[]).includes(raw as FilterValue);
    case 'not_in':
      return Array.isArray(rule.value) && !(rule.value as FilterValue[]).includes(raw as FilterValue);
    case 'contains':
      if (Array.isArray(raw)) return raw.includes(rule.value as string);
      return typeof raw === 'string' && raw.includes(rule.value as string);
    case 'before':
      return DATE_FIELDS.has(rule.field) && typeof raw === 'string' && raw !== '' && raw < (rule.value as string);
    case 'after':
      return DATE_FIELDS.has(rule.field) && typeof raw === 'string' && raw !== '' && raw > (rule.value as string);
    case 'any':
      return true;
    case 'exists':
      return raw !== null && raw !== undefined && raw !== '';
    case 'not_exists':
      return raw === null || raw === undefined || raw === '';
    default:
      return true;
  }
}

export function applyFilter(tasks: Task[], filterSet: FilterSet, ctx?: FilterContext): Task[] {
  if (filterSet.rules.length === 0) return tasks;
  return tasks.filter((task) => {
    const results = filterSet.rules.map((rule) => matchesRule(task, rule, ctx));
    return filterSet.conjunction === 'and'
      ? results.every(Boolean)
      : results.some(Boolean);
  });
}
