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
const DATE_FIELDS: ReadonlySet<keyof Task> = new Set(['dueDate', 'createdAt', 'updatedAt']);

export function matchesRule(task: Task, rule: FilterRule): boolean {
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

export function applyFilter(tasks: Task[], filterSet: FilterSet): Task[] {
  if (filterSet.rules.length === 0) return tasks;
  return tasks.filter((task) => {
    const results = filterSet.rules.map((rule) => matchesRule(task, rule));
    return filterSet.conjunction === 'and'
      ? results.every(Boolean)
      : results.some(Boolean);
  });
}
