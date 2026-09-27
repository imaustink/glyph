/**
 * Deleting a page or folder together with its tasks (api mode).
 *
 * The server refuses to delete a folder that holds pages other users own
 * (409 subtree_has_other_owners). When the user ticked "Delete All" in the
 * delete dialog, the tasks must survive that refusal: they may only go once
 * the folder itself is gone, or the notes that stay would point at tasks
 * that no longer exist.
 */

import { test, expect, switchUser } from './fixtures';
import type { APIRequestContext } from '@playwright/test';

const JSON_HEADERS = { 'X-Requested-With': 'XMLHttpRequest', 'Content-Type': 'application/json' };

async function post<T>(api: APIRequestContext, path: string, data: unknown): Promise<T> {
	const res = await api.post(path, { data, headers: JSON_HEADERS });
	expect(res.status(), await res.text()).toBe(201);
	return (await res.json()) as T;
}

test.describe('Delete with tasks (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('a refused folder delete leaves the tasks the user chose to delete with it', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');
		const api = page.request;
		await api.post(`${baseURL}/test/become/${seedUsers.userA.id}`);

		// Alice's folder, shared with Bob as an editor, with a note and a task.
		const folder = await post<{ id: string }>(api, '/api/v1/pages', { title: 'Team Folder', type: 'folder' });
		await post(api, '/api/v1/shares', {
			resourceType: 'folder',
			resourceId: folder.id,
			sharedWithId: seedUsers.userB.id,
			permission: 'editor'
		});
		const note = await post<{ id: string }>(api, '/api/v1/pages', { title: 'Alice Note', type: 'page', parentId: folder.id });
		const task = await post<{ id: string }>(api, '/api/v1/tasks', { title: 'Keep me', sourcePageId: note.id });

		// Bob files a page of his own inside Alice's folder.
		await api.post(`${baseURL}/test/become/${seedUsers.userB.id}`);
		await post(api, '/api/v1/pages', { title: 'Bob Page', type: 'page', parentId: folder.id });

		// Alice deletes the folder, choosing to delete its tasks too.
		await switchUser(page, baseURL, seedUsers.userA.id);
		const folderRow = page.locator('.node-row', { has: page.locator('.node-label:has-text("Team Folder")') });
		await expect(folderRow).toBeVisible({ timeout: 15_000 });
		await folderRow.hover();
		await folderRow.locator('.icon-btn[title="More options"]').click();
		await page.locator('.context-item:has-text("Delete")').click();
		const dialog = page.locator('[role="alertdialog"]');
		await expect(dialog).toBeVisible();
		await dialog.locator('button:has-text("Delete All")').click();

		// The server refuses: the folder holds Bob's page.
		await expect(page.locator('.toast')).toContainText('owned by other users', { timeout: 10_000 });
		await expect(folderRow).toBeVisible();

		// ...and the task is still there.
		const res = await api.get(`/api/v1/tasks/${task.id}`);
		expect(res.status(), 'task survives the refused delete').toBe(200);
	});

	test('"Keep Tasks" keeps a deleted note\'s tasks as standalone tasks', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');
		const api = page.request;
		await api.post(`${baseURL}/test/become/${seedUsers.userA.id}`);

		const note = await post<{ id: string }>(api, '/api/v1/pages', { title: 'Doomed Note', type: 'page' });
		const task = await post<{ id: string }>(api, '/api/v1/tasks', {
			title: 'Survivor',
			sourcePageId: note.id,
			status: 'in-progress'
		});

		await switchUser(page, baseURL, seedUsers.userA.id);
		const noteRow = page.locator('.node-row', { has: page.locator('.node-label:has-text("Doomed Note")') });
		await expect(noteRow).toBeVisible({ timeout: 15_000 });
		await noteRow.hover();
		await noteRow.locator('.icon-btn[title="More options"]').click();
		await page.locator('.context-item:has-text("Delete")').click();
		const dialog = page.locator('[role="alertdialog"]');
		await expect(dialog).toBeVisible();
		await dialog.locator('button:has-text("Keep Tasks")').click();
		await expect(noteRow).toHaveCount(0);

		// The note is gone; its task lives on, detached from it.
		expect((await api.get(`/api/v1/pages/${note.id}`)).status()).toBe(404);
		const res = await api.get(`/api/v1/tasks/${task.id}`);
		expect(res.status(), 'the kept task must survive the delete').toBe(200);
		const kept = (await res.json()) as { sourcePageId: string | null; status: string; title: string };
		expect(kept.sourcePageId).toBeNull();
		expect(kept.status).toBe('in-progress');
		expect(kept.title).toBe('Survivor');
	});
});
