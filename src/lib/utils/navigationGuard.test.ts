import { describe, it, expect } from 'vitest';
import { isNoteToNoteNavigation } from './navigationGuard';

describe('isNoteToNoteNavigation', () => {
	it('is true when moving from one note to another (#53)', () => {
		expect(isNoteToNoteNavigation('/notes/[pageId]', '/notes/[pageId]')).toBe(true);
	});

	it('is false when leaving the note page', () => {
		expect(isNoteToNoteNavigation('/notes/[pageId]', '/tasks')).toBe(false);
		expect(isNoteToNoteNavigation('/notes/[pageId]', '/tasks/folder/[folderId]')).toBe(false);
		expect(isNoteToNoteNavigation('/notes/[pageId]', null)).toBe(false);
	});

	it('is false when entering a note from another page', () => {
		expect(isNoteToNoteNavigation('/tasks/[taskId]', '/notes/[pageId]')).toBe(false);
		expect(isNoteToNoteNavigation(null, '/notes/[pageId]')).toBe(false);
		expect(isNoteToNoteNavigation(undefined, '/notes/[pageId]')).toBe(false);
	});
});
