/**
 * DI-30: uiStore.hasPendingWrites guards navigation and tab close. A write
 * still waiting on a debounce timer (content save, task title) counts: it
 * hasn't started, so neither the in-flight count nor a pending flush sees it.
 */
import { describe, it, expect } from 'vitest';
import { createUiStore } from './ui.svelte';

describe('uiStore.setPendingDebounce (DI-30)', () => {
	it('counts armed debounce timers as pending writes', () => {
		const store = createUiStore();
		expect(store.hasPendingWrites).toBe(false);

		store.setPendingDebounce('content-save', true);
		store.setPendingDebounce('task-title:t1', true);
		expect(store.hasPendingWrites).toBe(true);

		store.setPendingDebounce('content-save', false);
		expect(store.hasPendingWrites).toBe(true);
		store.setPendingDebounce('task-title:t1', false);
		expect(store.hasPendingWrites).toBe(false);
	});

	it('is idempotent per key', () => {
		const store = createUiStore();
		store.setPendingDebounce('k', true);
		store.setPendingDebounce('k', true);
		store.setPendingDebounce('k', false);
		expect(store.hasPendingWrites).toBe(false);
	});

	// The navigation guard waits with waitForSaveComplete and then retries;
	// resolving while a debounce is still armed made it cancel and retry again.
	it('waitForSaveComplete waits for armed debounces to be sent', async () => {
		const store = createUiStore();
		store.setPendingDebounce('content-save', true);
		let done = false;
		const wait = store.waitForSaveComplete(1000).then(() => { done = true; });
		await Promise.resolve();
		expect(done).toBe(false);

		store.setPendingDebounce('content-save', false);
		await wait;
		expect(done).toBe(true);
	});
});
