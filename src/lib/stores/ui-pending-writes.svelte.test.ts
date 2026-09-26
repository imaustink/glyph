/**
 * DI-30: uiStore.hasPendingWrites guards navigation and tab close. A write
 * still waiting on a debounce timer (content save, task title) counts: it
 * hasn't started, so neither the in-flight count nor a pending flush sees it.
 */
import { describe, it, expect } from 'vitest';
import { createUiStore } from './ui.svelte';

describe('uiStore.trackPendingWrite (DI-30)', () => {
	it('counts armed debounce timers as pending writes', () => {
		const store = createUiStore();
		expect(store.hasPendingWrites).toBe(false);

		store.trackPendingWrite('content-save', true);
		store.trackPendingWrite('task-title:t1', true);
		expect(store.hasPendingWrites).toBe(true);

		store.trackPendingWrite('content-save', false);
		expect(store.hasPendingWrites).toBe(true);
		store.trackPendingWrite('task-title:t1', false);
		expect(store.hasPendingWrites).toBe(false);
	});

	it('is idempotent per key', () => {
		const store = createUiStore();
		store.trackPendingWrite('k', true);
		store.trackPendingWrite('k', true);
		store.trackPendingWrite('k', false);
		expect(store.hasPendingWrites).toBe(false);
	});
});
