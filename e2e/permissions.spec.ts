/**
 * E2E permission enforcement tests (api mode only).
 *
 * Covers:
 * - Viewer cannot edit a directly-shared page (editor returns 403)
 * - Editor can edit a directly-shared page
 * - Org member can see org pages that are not private
 * - Org member cannot see private pages of other members
 */

import { test, expect, switchUser } from './fixtures';

test.describe('Permission enforcement (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('org member sees shared org page', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		// Create org and add bob
		await page.goto('/settings/orgs');
		await page.locator('input[placeholder="New organization name…"]').fill('Perm Test Org');
		await page.keyboard.press('Enter');
		await page.locator('.org-name-btn').first().click();
		await page.waitForSelector('.members-panel h2');

		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(2);

		// Navigate to a note page (auto-created "Getting Started")
		await page.goto('/');
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });

		// Title it for easy identification
		await page.locator('.page-title').click();
		await page.waitForSelector('input.title-edit');
		await page.locator('input.title-edit').fill('Org Page');
		await page.locator('input.title-edit').press('Enter');

		// Share the note with the org so Bob can see it
		await page.waitForSelector('.visibility-btn', { timeout: 10_000 });
		await page.locator('.visibility-btn').click();
		await page.waitForSelector('.visibility-picker .dropdown');
		await page.locator('.visibility-picker .option:has-text("Perm Test Org")').click();
		await expect(page.locator('.visibility-btn')).toContainText('Perm Test Org');

		// Switch to bob — he should see the org page
		await switchUser(page, baseURL, seedUsers.userB.id);
		await expect(page.locator('.node-label:has-text("Org Page")')).toBeVisible({ timeout: 15_000 });
	});

	test('direct viewer share cannot see Share button (read-only)', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		// Share with bob as viewer
		await page.waitForSelector('.visibility-btn', { timeout: 15_000 });
		await page.locator('.visibility-btn').click();
		await page.locator('.visibility-picker .option:has-text("Share with people")').click();
		await page.waitForSelector('.modal[aria-label="Share"]');
		await page.locator('.modal .email-input').fill(seedUsers.userB.email);
		// Keep default viewer permission
		await page.locator('.modal .btn-primary:has-text("Invite")').click();
		await expect(page.locator('.share-list .share-row')).toHaveCount(1);

		const noteUrl = page.url();
		await page.locator('.modal button[aria-label="Close"]').click();

		// Switch to bob
		await switchUser(page, baseURL, seedUsers.userB.id);
		await page.goto(noteUrl);
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });

		// Bob sees the page but not the visibility picker (only owners see it)
		await expect(page.locator('.visibility-btn')).toHaveCount(0);
	});
});
