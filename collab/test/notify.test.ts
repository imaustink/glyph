import { describe, it, expect } from 'vitest';
import { parseNotification } from '../src/notify.js';

const PAGE = '11111111-1111-4111-8111-111111111111';

describe('parseNotification', () => {
	it('parses a reset', () => {
		expect(parseNotification(JSON.stringify({ type: 'reset', pageId: PAGE.toUpperCase() }))).toEqual({ type: 'reset', pageId: PAGE });
	});

	it('parses a task status change', () => {
		expect(parseNotification(JSON.stringify({ type: 'task-status', pageId: PAGE, nodeId: 'n1', status: 'in-progress' }))).toEqual({
			type: 'task-status',
			pageId: PAGE,
			nodeId: 'n1',
			status: 'in-progress'
		});
	});

	it('rejects malformed or unknown payloads', () => {
		for (const bad of [
			undefined,
			'not json',
			JSON.stringify({ type: 'reset' }),
			JSON.stringify({ type: 'task-status', pageId: PAGE, nodeId: 'n1', status: 'bogus' }),
			JSON.stringify({ type: 'task-status', pageId: PAGE, status: 'done' }),
			JSON.stringify({ type: 'something-else', pageId: PAGE })
		]) {
			expect(parseNotification(bad)).toBeNull();
		}
	});
});
