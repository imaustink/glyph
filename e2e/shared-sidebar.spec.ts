/**
 * E2E tests for the "Shared with me" sidebar section (api mode only).
 *
 * Before this feature, a folder or note shared with you was reachable only by
 * its URL: shared folders were never returned by the page list, and a note
 * shared from inside a folder was orphaned under a parent you can't see, so it
 * rendered nowhere. These tests drive the real sharing UI as the owner (Alice),
 * then switch to the recipient (Bob) and assert the shared items surface in the
 * new sidebar section and link to the right place.
 *
 * Covers:
 * - A shared folder appears in the recipient's "Shared with me" section and
 *   links to its folder board; a viewer share shows the "View" badge.
 * - A note shared from inside a folder (the orphan case) surfaces in the
 *   section even though it renders nowhere in the main page tree.
 * - The section is absent for a user with nothing shared with them.
 * - Revoking a share removes it from the recipient's section.
 * - An editor share shows no "View" badge.
 */

import { test, expect, switchUser, createNewFolder, createNewPage, waitForEditorReady } from './fixtures';
import type { Page } from '@playwright/test';

/** Become one of the seed users and wait for their sidebar to load. */
async function become(page: Page, baseURL: string, userId: string) {
	await switchUser(page, baseURL, userId);
	await page.waitForSelector('.app-shell .sidebar', { timeout: 30_000 });
}

/** Open the folder board for the (single) folder in the current sidebar. */
async function openFolderBoard(page: Page) {
	const folderRow = page.locator('.node-row.folder').first();
	await expect(folderRow).toBeVisible({ timeout: 10_000 });
	await folderRow.hover();
	await folderRow.locator('button[title="Open folder board"]').click();
	await page.waitForSelector('.board-page', { timeout: 15_000 });
}

/** Share the open folder board with the given email at the given permission. */
async function shareOpenFolderWith(
	page: Page,
	email: string,
	permission: 'viewer' | 'editor' = 'viewer'
) {
	await page.locator('.share-btn').click();
	await page.waitForSelector('.modal[aria-label="Share"]');
	await page.locator('.modal .email-input').fill(email);
	if (permission === 'editor') {
		await page.locator('.add-section .perm-select').selectOption('editor');
	}
	await page.locator('.modal .btn-primary:has-text("Invite")').click();
	await expect(page.locator('.share-list .share-row')).toHaveCount(1);
	await page.locator('.modal button[aria-label="Close"]').click();
	await expect(page.locator('.modal[aria-label="Share"]')).not.toBeVisible();
}

test.describe('Shared with me sidebar (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('shared folder surfaces in the recipient sidebar and links to its board', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');
		const { userA, userB } = seedUsers;

		// As Alice: make a folder and share it with Bob from its board.
		await become(page, baseURL, userA.id);
		await createNewFolder(page);
		await openFolderBoard(page);
		const folderUrl = page.url();
		await shareOpenFolderWith(page, userB.email, 'viewer');

		// As Bob: the folder shows under "Shared with me" with a view-only badge.
		await become(page, baseURL, userB.id);
		await expect(page.locator('.section-label:has-text("Shared with me")')).toBeVisible();
		const row = page.locator('.shared-list .shared-row', { hasText: 'New Folder' });
		await expect(row).toBeVisible();
		await expect(row.locator('.shared-badge')).toHaveText('View');

		// Clicking it opens the shared folder board.
		await row.click();
		await page.waitForURL((url) => url.pathname.startsWith('/tasks/folder/'), { timeout: 15_000 });
		expect(page.url()).toBe(folderUrl);
		await expect(page.locator('.board-page')).toBeVisible();
	});

	test('note shared from inside a folder surfaces even though it is orphaned in the tree', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');
		const { userA, userB } = seedUsers;

		// As Alice: make a folder, add a note inside it, title and share the note.
		await become(page, baseURL, userA.id);
		await createNewFolder(page);
		const folderRow = page.locator('.node-row.folder').first();
		await expect(folderRow).toBeVisible({ timeout: 10_000 });
		await folderRow.locator('.node-label').click({ button: 'right' });
		await page.locator('.context-item:has-text("New page inside")').click();
		await page.waitForURL((url) => url.pathname.startsWith('/notes/'), { timeout: 15_000 });
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		await waitForEditorReady(page);

		await page.locator('.page-title').click();
		await page.waitForSelector('input.title-edit');
		await page.locator('input.title-edit').fill('Nested Note');
		await page.locator('input.title-edit').press('Enter');

		// Share the note via its visibility picker (resourceType = page).
		await page.locator('.visibility-btn').click();
		await page.locator('.visibility-picker .option:has-text("Share with people")').click();
		await page.waitForSelector('.modal[aria-label="Share"]');
		await page.locator('.modal .email-input').fill(userB.email);
		await page.locator('.modal .btn-primary:has-text("Invite")').click();
		await expect(page.locator('.share-list .share-row')).toHaveCount(1);
		await page.locator('.modal button[aria-label="Close"]').click();

		// As Bob: the note is NOT in the main page tree (its parent folder is
		// invisible to him), but it does appear under "Shared with me".
		await become(page, baseURL, userB.id);
		await expect(
			page.locator('.page-tree-container .node-label:has-text("Nested Note")')
		).toHaveCount(0);
		const row = page.locator('.shared-list .shared-row', { hasText: 'Nested Note' });
		await expect(row).toBeVisible();

		// Clicking it opens the note.
		await row.click();
		await page.waitForURL((url) => url.pathname.startsWith('/notes/'), { timeout: 15_000 });
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
	});

	test('a note shared at the owner root renders once (tree only, not duplicated in the section)', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');
		const { userA, userB } = seedUsers;

		// As Alice: create a root-level note (parent = null), title and share it.
		await become(page, baseURL, userA.id);
		await createNewPage(page);
		await page.waitForSelector('input.title-edit');
		await page.locator('input.title-edit').fill('Root Shared Note');
		await page.locator('input.title-edit').press('Enter');

		await page.locator('.visibility-btn').click();
		await page.locator('.visibility-picker .option:has-text("Share with people")').click();
		await page.waitForSelector('.modal[aria-label="Share"]');
		await page.locator('.modal .email-input').fill(userB.email);
		await page.locator('.modal .btn-primary:has-text("Invite")').click();
		await expect(page.locator('.share-list .share-row')).toHaveCount(1);
		await page.locator('.modal button[aria-label="Close"]').click();

		// As Bob: a root-shared note is reachable in his main tree, so it renders
		// there exactly once and is NOT duplicated in "Shared with me". With it
		// being the only share, the section is absent entirely.
		await become(page, baseURL, userB.id);
		await expect(
			page.locator('.page-tree-container .node-label:has-text("Root Shared Note")')
		).toHaveCount(1);
		await expect(page.locator('.shared-list-container')).toHaveCount(0);
		await expect(page.locator('.section-label:has-text("Shared with me")')).toHaveCount(0);
	});

	test('section is absent for a user with nothing shared with them', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');
		const { userB } = seedUsers;

		await become(page, baseURL, userB.id);
		await expect(page.locator('.shared-list-container')).toHaveCount(0);
		await expect(page.locator('.section-label:has-text("Shared with me")')).toHaveCount(0);
	});

	test('revoking a share removes it from the recipient section', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');
		const { userA, userB } = seedUsers;

		// As Alice: share a folder with Bob.
		await become(page, baseURL, userA.id);
		await createNewFolder(page);
		await openFolderBoard(page);
		await shareOpenFolderWith(page, userB.email, 'viewer');

		// Bob sees it.
		await become(page, baseURL, userB.id);
		await expect(
			page.locator('.shared-list .shared-row', { hasText: 'New Folder' })
		).toBeVisible();

		// As Alice: revoke the share from the folder board's share dialog.
		await become(page, baseURL, userA.id);
		await openFolderBoard(page);
		await page.locator('.share-btn').click();
		await page.waitForSelector('.modal[aria-label="Share"]');
		await page.locator('.share-list .remove-btn').click();
		await expect(page.locator('.share-list .share-row')).toHaveCount(0);
		await page.locator('.modal button[aria-label="Close"]').click();

		// Bob no longer has a shared section.
		await become(page, baseURL, userB.id);
		await expect(page.locator('.shared-list-container')).toHaveCount(0);
	});

	test('an editor share shows no view-only badge', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');
		const { userA, userB } = seedUsers;

		await become(page, baseURL, userA.id);
		await createNewFolder(page);
		await openFolderBoard(page);
		await shareOpenFolderWith(page, userB.email, 'editor');

		await become(page, baseURL, userB.id);
		const row = page.locator('.shared-list .shared-row', { hasText: 'New Folder' });
		await expect(row).toBeVisible();
		await expect(row.locator('.shared-badge')).toHaveCount(0);
	});
});
