/**
 * Tests for optimistic-concurrency bookkeeping in the pages store.
 *
 * The store remembers the revision it last observed for each page and sends it
 * back as `expectedRevision`, so the server can reject a write derived from a
 * stale read instead of silently overwriting newer content.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createPagesStore } from './pages.svelte';
import type { IPageRepository } from '$lib/storage/interfaces';
import type { PageContent, TreeNode } from '$lib/models/types';

function createMockRepo(overrides: Partial<IPageRepository> = {}): IPageRepository {
	return {
		getAll: vi.fn().mockResolvedValue([]),
		getById: vi.fn().mockResolvedValue(null),
		create: vi.fn().mockImplementation(async (n: TreeNode) => n),
		update: vi.fn().mockResolvedValue(null),
		delete: vi.fn().mockResolvedValue(true),
		upsert: vi.fn().mockImplementation(async (n: TreeNode) => n),
		deleteMany: vi.fn().mockResolvedValue(undefined),
		getContent: vi.fn().mockResolvedValue(null),
		saveContent: vi.fn().mockResolvedValue(undefined),
		deleteContent: vi.fn().mockResolvedValue(undefined),
		deleteWithContent: vi.fn().mockResolvedValue(true),
		deleteSubtree: vi.fn().mockResolvedValue(undefined),
		getTree: vi.fn().mockReturnValue([]),
		getChildren: vi.fn().mockReturnValue([]),
		...overrides
	};
}

function content(revision?: number): PageContent {
	return {
		pageId: 'page-1',
		content: { type: 'doc' },
		updatedAt: '2026-01-01T00:00:00Z',
		...(revision !== undefined ? { revision } : {})
	};
}

describe('pagesStore content revisions', () => {
	let repo: ReturnType<typeof createMockRepo>;
	let store: ReturnType<typeof createPagesStore>;

	beforeEach(() => {
		repo = createMockRepo();
		store = createPagesStore(repo);
	});

	it('sends no precondition before any revision has been observed', async () => {
		await store.saveContent('page-1', { type: 'doc' });
		const arg = (repo.saveContent as ReturnType<typeof vi.fn>).mock.calls[0][0];
		expect(arg).not.toHaveProperty('expectedRevision');
	});

	it('echoes the revision observed from getContent on the next save', async () => {
		(repo.getContent as ReturnType<typeof vi.fn>).mockResolvedValue(content(7));
		await store.getContent('page-1');

		await store.saveContent('page-1', { type: 'doc' });
		const arg = (repo.saveContent as ReturnType<typeof vi.fn>).mock.calls[0][0];
		expect(arg.expectedRevision).toBe(7);
	});

	it('advances the precondition using the revision returned by a save', async () => {
		(repo.getContent as ReturnType<typeof vi.fn>).mockResolvedValue(content(1));
		await store.getContent('page-1');

		(repo.saveContent as ReturnType<typeof vi.fn>).mockResolvedValue(content(2));
		await store.saveContent('page-1', { type: 'doc' });

		await store.saveContent('page-1', { type: 'doc' });
		const second = (repo.saveContent as ReturnType<typeof vi.fn>).mock.calls[1][0];
		expect(second.expectedRevision).toBe(2);
	});

	it('drops the precondition when a save returns no revision (local mode)', async () => {
		(repo.getContent as ReturnType<typeof vi.fn>).mockResolvedValue(content(4));
		await store.getContent('page-1');

		// Local repository resolves void.
		(repo.saveContent as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
		await store.saveContent('page-1', { type: 'doc' });

		await store.saveContent('page-1', { type: 'doc' });
		const second = (repo.saveContent as ReturnType<typeof vi.fn>).mock.calls[1][0];
		// Reusing revision 4 would conflict forever; the write must be unconditional.
		expect(second).not.toHaveProperty('expectedRevision');
	});

	it('forgetRevision clears the precondition', async () => {
		(repo.getContent as ReturnType<typeof vi.fn>).mockResolvedValue(content(9));
		await store.getContent('page-1');

		store.forgetRevision('page-1');
		await store.saveContent('page-1', { type: 'doc' });

		const arg = (repo.saveContent as ReturnType<typeof vi.fn>).mock.calls[0][0];
		expect(arg).not.toHaveProperty('expectedRevision');
	});

	it('tracks revisions per page independently', async () => {
		(repo.getContent as ReturnType<typeof vi.fn>).mockImplementation(async (id: string) => ({
			pageId: id,
			content: { type: 'doc' },
			updatedAt: '2026-01-01T00:00:00Z',
			revision: id === 'page-1' ? 3 : 11
		}));
		await store.getContent('page-1');
		await store.getContent('page-2');

		await store.saveContent('page-2', { type: 'doc' });
		const arg = (repo.saveContent as ReturnType<typeof vi.fn>).mock.calls[0][0];
		expect(arg.pageId).toBe('page-2');
		expect(arg.expectedRevision).toBe(11);
	});
});
