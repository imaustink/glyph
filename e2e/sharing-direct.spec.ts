/**
 * E2E tests for direct resource sharing (api mode only).
 *
 * Covers:
 * - Visibility picker visible on notes pages in api mode
 * - Opening and closing the ShareDialog via the visibility picker
 * - Inviting a user via the share dialog
 * - Shared user can see the page
 * - Removing a share removes access
 * - Permission change (viewer → editor)
 */

import { test, expect, switchUser } from './fixtures';

/** Open the ShareDialog via the visibility picker dropdown. */
async function openShareDialog(page: import('@playwright/test').Page) {
	await page.waitForSelector('.visibility-btn', { timeout: 15_000 });
	await page.locator('.visibility-btn').click();
	await page.locator('.visibility-picker .option:has-text("Share with people")').click();
	await page.waitForSelector('.modal[aria-label="Share"]');
}

test.describe('Direct Sharing (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('Visibility picker is visible on a note page', async ({ page }) => {
		// Layout navigates to first note automatically
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		await expect(page.locator('.visibility-btn')).toBeVisible();
	});

	test('opens and closes the ShareDialog', async ({ page }) => {
		await openShareDialog(page);
		await expect(page.locator('.modal[aria-label="Share"]')).toBeVisible();

		// Close via X button
		await page.locator('.modal button[aria-label="Close"]').click();
		await expect(page.locator('.modal[aria-label="Share"]')).not.toBeVisible();
	});

	test('closes ShareDialog on Escape key', async ({ page }) => {
		await openShareDialog(page);
		await expect(page.locator('.modal[aria-label="Share"]')).toBeVisible();
		await page.keyboard.press('Escape');
		await expect(page.locator('.modal[aria-label="Share"]')).not.toBeVisible();
	});

	test('invites a user to a page', async ({ page, seedUsers }) => {
		if (!seedUsers) throw new Error('seedUsers missing');

		await openShareDialog(page);

		// Enter bob's email
		await page.locator('.modal .email-input').fill(seedUsers.userB.email);

		await page.locator('.modal .btn-primary:has-text("Invite")').click();

		// Share row appears
		await expect(page.locator('.share-list .share-row')).toHaveCount(1);
	});

	test('shared user can see the page in the sidebar', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		// Create page and share with bob
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		// Title the page for identification
		await page.locator('.page-title').click();
		await page.waitForSelector('input.title-edit');
		await page.locator('input.title-edit').fill('Shared With Bob');
		await page.locator('input.title-edit').press('Enter');

		await openShareDialog(page);
		await page.locator('.modal .email-input').fill(seedUsers.userB.email);
		await page.locator('.modal .btn-primary:has-text("Invite")').click();
		await expect(page.locator('.share-list .share-row')).toHaveCount(1);
		await page.locator('.modal button[aria-label="Close"]').click();

		// Switch to bob
		await switchUser(page, baseURL, seedUsers.userB.id);

		// Bob should see the shared page in his sidebar
		await expect(page.locator(`.node-label:has-text("Shared With Bob")`)).toBeVisible();
	});

	test('removing a share removes visibility for that user', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });

		// Share with bob
		await openShareDialog(page);
		await page.locator('.modal .email-input').fill(seedUsers.userB.email);
		await page.locator('.modal .btn-primary:has-text("Invite")').click();
		await expect(page.locator('.share-list .share-row')).toHaveCount(1);

		// Remove the share
		await page.locator('.share-list .remove-btn').click();
		await expect(page.locator('.share-list .share-row')).toHaveCount(0);
		await page.locator('.modal button[aria-label="Close"]').click();

		// Switch to bob — should see "Not shared with anyone yet" message on that page
		const noteUrl = page.url();
		await switchUser(page, baseURL, seedUsers.userB.id);
		await page.goto(noteUrl);
		// Bob sees not-found (404 means the page lookup returns nothing)
		await expect(page.locator('.not-found')).toBeVisible();
	});

	test('can change share permission from viewer to editor', async ({ page, seedUsers }) => {
		if (!seedUsers) throw new Error('seedUsers missing');

		await openShareDialog(page);

		// Invite bob as viewer
		await page.locator('.modal .email-input').fill(seedUsers.userB.email);
		// Default is viewer; change to editor
		await page.locator('.add-section .perm-select').selectOption('editor');
		await page.locator('.modal .btn-primary:has-text("Invite")').click();
		await expect(page.locator('.share-list .share-row')).toHaveCount(1);

		// Change to viewer
		await page.locator('.share-list .perm-select').selectOption('viewer');
		// The row stays but the select changes
		await expect(page.locator('.share-list .perm-select')).toHaveValue('viewer');
	});
});
