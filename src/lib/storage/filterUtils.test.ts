/**
 * Unit tests for filterUtils — matchesRule and applyFilter.
 *
 * Covers all operators and verifies that before/after respect the DATE_FIELDS
 * guard (non-date fields always return false regardless of the value).
 */

import { describe, it, expect } from 'vitest';
import { matchesRule, applyFilter } from './filterUtils';
import type { Task, FilterRule, FilterSet } from '$lib/models/types';

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    title: 'Test Task',
    description: '',
    status: 'todo',
    priority: 'none',
    tags: [],
    dueDate: null,
    sourcePageId: null,
    sourceNodeId: null,
    link: null,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    order: 0,
    ...overrides
  };
}

function rule(overrides: Partial<FilterRule>): FilterRule {
  return { id: 'r1', field: 'status', operator: 'eq', value: null, ...overrides };
}

// ─── matchesRule ──────────────────────────────────────────────────────────────

describe('matchesRule', () => {
  describe('eq', () => {
    it('returns true when field equals value', () => {
      expect(matchesRule(makeTask({ status: 'done' }), rule({ field: 'status', operator: 'eq', value: 'done' }))).toBe(true);
    });
    it('returns false when field does not equal value', () => {
      expect(matchesRule(makeTask({ status: 'todo' }), rule({ field: 'status', operator: 'eq', value: 'done' }))).toBe(false);
    });
  });

  describe('neq', () => {
    it('returns true when field does not equal value', () => {
      expect(matchesRule(makeTask({ status: 'todo' }), rule({ field: 'status', operator: 'neq', value: 'done' }))).toBe(true);
    });
    it('returns false when field equals value', () => {
      expect(matchesRule(makeTask({ status: 'done' }), rule({ field: 'status', operator: 'neq', value: 'done' }))).toBe(false);
    });
  });

  describe('in', () => {
    it('returns true when value array includes field value', () => {
      expect(matchesRule(makeTask({ status: 'done' }), rule({ field: 'status', operator: 'in', value: ['todo', 'done'] }))).toBe(true);
    });
    it('returns false when value array does not include field value', () => {
      expect(matchesRule(makeTask({ status: 'cancelled' }), rule({ field: 'status', operator: 'in', value: ['todo', 'done'] }))).toBe(false);
    });
    it('returns false when rule.value is not an array', () => {
      expect(matchesRule(makeTask({ status: 'done' }), rule({ field: 'status', operator: 'in', value: 'done' }))).toBe(false);
    });
  });

  describe('not_in', () => {
    it('returns true when value array does not include field value', () => {
      expect(matchesRule(makeTask({ status: 'cancelled' }), rule({ field: 'status', operator: 'not_in', value: ['todo', 'done'] }))).toBe(true);
    });
    it('returns false when value array includes field value', () => {
      expect(matchesRule(makeTask({ status: 'done' }), rule({ field: 'status', operator: 'not_in', value: ['todo', 'done'] }))).toBe(false);
    });
    it('returns false when rule.value is not an array', () => {
      expect(matchesRule(makeTask({ status: 'done' }), rule({ field: 'status', operator: 'not_in', value: 'done' }))).toBe(false);
    });
  });

  describe('contains (array field)', () => {
    it('returns true when tags array contains the value', () => {
      expect(matchesRule(makeTask({ tags: ['bug', 'feature'] }), rule({ field: 'tags', operator: 'contains', value: 'bug' }))).toBe(true);
    });
    it('returns false when tags array does not contain the value', () => {
      expect(matchesRule(makeTask({ tags: ['feature'] }), rule({ field: 'tags', operator: 'contains', value: 'bug' }))).toBe(false);
    });
  });

  describe('contains (string field)', () => {
    it('returns true when title contains the substring', () => {
      expect(matchesRule(makeTask({ title: 'Fix the bug today' }), rule({ field: 'title', operator: 'contains', value: 'bug' }))).toBe(true);
    });
    it('returns false when title does not contain the substring', () => {
      expect(matchesRule(makeTask({ title: 'Deploy to staging' }), rule({ field: 'title', operator: 'contains', value: 'bug' }))).toBe(false);
    });
  });

  describe('before', () => {
    it('returns true when dueDate (DATE_FIELD) is before the value', () => {
      expect(matchesRule(makeTask({ dueDate: '2026-03-01' }), rule({ field: 'dueDate', operator: 'before', value: '2026-06-01' }))).toBe(true);
    });
    it('returns false when dueDate is after the value', () => {
      expect(matchesRule(makeTask({ dueDate: '2026-09-01' }), rule({ field: 'dueDate', operator: 'before', value: '2026-06-01' }))).toBe(false);
    });
    it('returns false when dueDate is empty string', () => {
      expect(matchesRule(makeTask({ dueDate: '' as unknown as null }), rule({ field: 'dueDate', operator: 'before', value: '2026-06-01' }))).toBe(false);
    });
    it('returns false on a non-date field even if string comparison would pass', () => {
      // 'aaa' < 'bbb' is true lexicographically, but title is not a DATE_FIELD
      expect(matchesRule(makeTask({ title: 'aaa' }), rule({ field: 'title', operator: 'before', value: 'bbb' }))).toBe(false);
    });
  });

  describe('after', () => {
    it('returns true when dueDate (DATE_FIELD) is after the value', () => {
      expect(matchesRule(makeTask({ dueDate: '2026-09-01' }), rule({ field: 'dueDate', operator: 'after', value: '2026-06-01' }))).toBe(true);
    });
    it('returns false when dueDate is before the value', () => {
      expect(matchesRule(makeTask({ dueDate: '2026-03-01' }), rule({ field: 'dueDate', operator: 'after', value: '2026-06-01' }))).toBe(false);
    });
    it('returns false when dueDate is empty string', () => {
      expect(matchesRule(makeTask({ dueDate: '' as unknown as null }), rule({ field: 'dueDate', operator: 'after', value: '2026-01-01' }))).toBe(false);
    });
    it('returns false on a non-date field even if string comparison would pass', () => {
      // 'zzz' > 'aaa' is true lexicographically, but title is not a DATE_FIELD
      expect(matchesRule(makeTask({ title: 'zzz' }), rule({ field: 'title', operator: 'after', value: 'aaa' }))).toBe(false);
    });
  });

  describe('any', () => {
    it('always returns true regardless of field value', () => {
      expect(matchesRule(makeTask({ status: 'cancelled' }), rule({ field: 'status', operator: 'any', value: null }))).toBe(true);
      expect(matchesRule(makeTask({ dueDate: null }), rule({ field: 'dueDate', operator: 'any', value: null }))).toBe(true);
    });
  });

  describe('exists', () => {
    it('returns true when field has a non-empty value', () => {
      expect(matchesRule(makeTask({ dueDate: '2026-06-01' }), rule({ field: 'dueDate', operator: 'exists', value: null }))).toBe(true);
    });
    it('returns false when field is null', () => {
      expect(matchesRule(makeTask({ dueDate: null }), rule({ field: 'dueDate', operator: 'exists', value: null }))).toBe(false);
    });
    it('returns false when field is empty string', () => {
      expect(matchesRule(makeTask({ title: '' }), rule({ field: 'title', operator: 'exists', value: null }))).toBe(false);
    });
  });

  describe('not_exists', () => {
    it('returns true when field is null', () => {
      expect(matchesRule(makeTask({ dueDate: null }), rule({ field: 'dueDate', operator: 'not_exists', value: null }))).toBe(true);
    });
    it('returns true when field is empty string', () => {
      expect(matchesRule(makeTask({ title: '' }), rule({ field: 'title', operator: 'not_exists', value: null }))).toBe(true);
    });
    it('returns false when field has a non-empty value', () => {
      expect(matchesRule(makeTask({ dueDate: '2026-06-01' }), rule({ field: 'dueDate', operator: 'not_exists', value: null }))).toBe(false);
    });
  });

  describe('default (unknown operator)', () => {
    it('returns true for an unrecognised operator', () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      expect(matchesRule(makeTask(), rule({ operator: 'unknown' as any }))).toBe(true);
    });
  });
});

// ─── applyFilter ─────────────────────────────────────────────────────────────

describe('applyFilter', () => {
  const tasks = [
    makeTask({ id: 't1', status: 'todo', priority: 'high', tags: ['bug'], dueDate: '2026-03-01' }),
    makeTask({ id: 't2', status: 'done', priority: 'low', tags: [], dueDate: '2026-06-01' }),
    makeTask({ id: 't3', status: 'todo', priority: 'none', tags: ['bug', 'feature'], dueDate: null })
  ];

  it('returns all tasks when rules array is empty', () => {
    const fs: FilterSet = { conjunction: 'and', rules: [] };
    expect(applyFilter(tasks, fs)).toHaveLength(3);
  });

  it('filters with AND conjunction (all rules must match)', () => {
    const fs: FilterSet = {
      conjunction: 'and',
      rules: [
        { id: 'r1', field: 'status', operator: 'eq', value: 'todo' },
        { id: 'r2', field: 'priority', operator: 'eq', value: 'high' }
      ]
    };
    const result = applyFilter(tasks, fs);
    expect(result.map((t) => t.id)).toEqual(['t1']);
  });

  it('filters with OR conjunction (any rule must match)', () => {
    const fs: FilterSet = {
      conjunction: 'or',
      rules: [
        { id: 'r1', field: 'status', operator: 'eq', value: 'done' },
        { id: 'r2', field: 'priority', operator: 'eq', value: 'high' }
      ]
    };
    const result = applyFilter(tasks, fs);
    expect(result.map((t) => t.id).sort()).toEqual(['t1', 't2']);
  });

  it('returns no tasks when no tasks match all AND rules', () => {
    const fs: FilterSet = {
      conjunction: 'and',
      rules: [
        { id: 'r1', field: 'status', operator: 'eq', value: 'done' },
        { id: 'r2', field: 'priority', operator: 'eq', value: 'high' }
      ]
    };
    expect(applyFilter(tasks, fs)).toHaveLength(0);
  });

  it('applies before on dueDate correctly', () => {
    const fs: FilterSet = {
      conjunction: 'and',
      rules: [{ id: 'r1', field: 'dueDate', operator: 'before', value: '2026-05-01' }]
    };
    expect(applyFilter(tasks, fs).map((t) => t.id)).toEqual(['t1']);
  });

  it('applies after on dueDate correctly', () => {
    const fs: FilterSet = {
      conjunction: 'and',
      rules: [{ id: 'r1', field: 'dueDate', operator: 'after', value: '2026-05-01' }]
    };
    expect(applyFilter(tasks, fs).map((t) => t.id)).toEqual(['t2']);
  });
});
