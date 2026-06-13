/**
 * E2E tests for folder board (api mode only).
 *
 * Covers:
 * - Owner can open the folder board via sidebar icon
 * - Owner can add lanes
 * - Viewer can access the board URL but cannot add lanes
 * - Editor can add lanes when accessing the board URL
 */

import { test, expect, createNewFolder, switchUser } from './fixtures';

/** Navigate to the folder board for the first folder by clicking its board icon. */
async function openFolderBoard(page: import('@playwright/test').Page) {
	// .node-row.folder is the sidebar row for folder nodes; board icon appears on hover.
	const folderRow = page.locator('.node-row.folder').first();
	await folderRow.waitFor({ state: 'visible', timeout: 10_000 });
	await folderRow.hover();
	await folderRow.locator('button[title="Open folder board"]').click();
	await page.waitForSelector('.board-page', { timeout: 15_000 });
}

/** Wait for the board to be fully loaded (canEdit resolved). */
async function waitForBoardLoad(page: import('@playwright/test').Page) {
	// The folder title switches from "Loading…" to the real name once the API call completes.
	await expect(page.locator('.board-title')).not.toHaveText('Loading…', { timeout: 10_000 });
}

test.describe('Folder Board (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('owner can open folder board via sidebar icon', async ({ page }) => {
		await createNewFolder(page);

		// Board icon appears on hover of the folder row.
		await openFolderBoard(page);

		// The board page renders and shows the folder's name.
		await expect(page.locator('.board-title')).toBeVisible({ timeout: 10_000 });
		await expect(page.locator('.board-title')).toHaveText('New Folder');
	});

	test('owner can add lanes to the folder board', async ({ page }) => {
		await createNewFolder(page);
		await openFolderBoard(page);

		// Wait for the API load to complete — button only renders when canEdit is true.
		await page.locator('.add-lane-btn').waitFor({ state: 'visible', timeout: 10_000 });
		await page.locator('.add-lane-btn').click();

		// Lane component root element has class="lane".
		await expect(page.locator('.lane')).toHaveCount(1, { timeout: 8_000 });
	});

	test('viewer cannot add lanes on shared folder board', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		await createNewFolder(page);
		await openFolderBoard(page);

		// Wait for the board to fully load (canEdit resolved) so .share-btn is rendered.
		await waitForBoardLoad(page);

		// Capture the board URL so we can navigate directly after switching users.
		// (Shared folders don't appear in the recipient's page tree, but the API
		//  does allow access when a folder share row exists for that user.)
		const boardUrl = page.url();

		// Share folder with userB as viewer (default permission).
		await page.locator('.share-btn').click();
		await page.waitForSelector('.modal[aria-label="Share"]', { timeout: 8_000 });
		await page.locator('.modal .email-input').fill(seedUsers.userB.email);
		await page.locator('.modal .btn-primary:has-text("Invite")').click();
		await expect(page.locator('.share-list .share-row')).toHaveCount(1, { timeout: 8_000 });
		await page.locator('.modal button[aria-label="Close"]').click();

		// Switch to userB and navigate directly to the board URL.
		await switchUser(page, baseURL, seedUsers.userB.id);
		await page.goto(boardUrl, { waitUntil: 'commit' });
		await page.waitForSelector('.board-page', { timeout: 15_000 });
		await waitForBoardLoad(page);

		// "Add lane" button should NOT be visible for viewer.
		await expect(page.locator('.add-lane-btn')).not.toBeVisible();
	});

	test('editor can add lanes on shared folder board', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		await createNewFolder(page);
		await openFolderBoard(page);

		// Wait for the board to fully load (canEdit resolved) so .share-btn is rendered.
		await waitForBoardLoad(page);

		// Capture the board URL before switching users.
		const boardUrl = page.url();

		// Share folder with userB as editor.
		await page.locator('.share-btn').click();
		await page.waitForSelector('.modal[aria-label="Share"]', { timeout: 8_000 });
		await page.locator('.modal .email-input').fill(seedUsers.userB.email);
		await page.locator('.modal .btn-primary:has-text("Invite")').click();
		await expect(page.locator('.share-list .share-row')).toHaveCount(1, { timeout: 8_000 });
		// Change permission to editor — the select inside the share row has class "perm-select".
		await page.locator('.share-list .share-row .perm-select').selectOption('editor');
		await page.locator('.modal button[aria-label="Close"]').click();

		// Switch to userB and navigate directly to the board URL.
		await switchUser(page, baseURL, seedUsers.userB.id);
		await page.goto(boardUrl, { waitUntil: 'commit' });
		await page.waitForSelector('.board-page', { timeout: 15_000 });

		// "Add lane" button should be visible for editor.
		await page.locator('.add-lane-btn').waitFor({ state: 'visible', timeout: 10_000 });
		await expect(page.locator('.add-lane-btn')).toBeVisible();
	});
});

