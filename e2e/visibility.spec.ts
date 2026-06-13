/**
 * E2E tests for note visibility controls (api mode only).
 *
 * Covers:
 * - New notes default to Private
 * - Visibility picker is visible on note pages
 * - Changing visibility to an org makes the note visible to org members
 * - Changing back to Private hides the note from org members
 */

import { test, expect, switchUser } from './fixtures';

test.describe('Note Visibility (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('new note defaults to Private', async ({ page }) => {
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		// Visibility picker should show "Private"
		await expect(page.locator('.visibility-btn')).toContainText('Private');
	});

	test('visibility picker is visible on note pages', async ({ page }) => {
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		await expect(page.locator('.visibility-btn')).toBeVisible();
	});

	test('can open visibility dropdown and see Private option', async ({ page }) => {
		await page.waitForSelector('.visibility-btn', { timeout: 15_000 });
		await page.locator('.visibility-btn').click();
		await expect(page.locator('.visibility-picker .dropdown')).toBeVisible();
		await expect(page.locator('.visibility-picker .option.selected')).toContainText('Private');
	});

	test('org member cannot see private note', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		// Title the current (private) note
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		await page.locator('.page-title').click();
		await page.waitForSelector('input.title-edit');
		await page.locator('input.title-edit').fill('Private Note');
		await page.locator('input.title-edit').press('Enter');

		const noteUrl = page.url();

		// Create an org and add bob
		await page.goto('/settings/orgs');
		await page.locator('input[placeholder="New organization name…"]').fill('Visibility Test Org');
		await page.keyboard.press('Enter');
		await page.locator('.org-name-btn').first().click();
		await page.waitForSelector('.members-panel h2');
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(2);

		// Switch to bob — the private note should NOT be visible
		await switchUser(page, baseURL, seedUsers.userB.id);
		await page.goto(noteUrl);
		// Bob should get a not-found since the note is private
		await expect(page.locator('.not-found')).toBeVisible({ timeout: 10_000 });
	});

	test('sharing with org makes note visible to org member', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		// Title the current note
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		await page.locator('.page-title').click();
		await page.waitForSelector('input.title-edit');
		await page.locator('input.title-edit').fill('Org Visible Note');
		await page.locator('input.title-edit').press('Enter');

		// Create an org and add bob
		await page.goto('/settings/orgs');
		await page.locator('input[placeholder="New organization name…"]').fill('Share Test Org');
		await page.keyboard.press('Enter');
		await page.locator('.org-name-btn').first().click();
		await page.waitForSelector('.members-panel h2');
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(2);

		// Navigate back to the note and change visibility to the org
		await page.goto('/');
		await page.waitForSelector('.node-label:has-text("Org Visible Note")', { timeout: 15_000 });
		await page.locator('.node-label:has-text("Org Visible Note")').click();
		await page.waitForSelector('.visibility-btn', { timeout: 15_000 });

		await page.locator('.visibility-btn').click();
		await page.waitForSelector('.visibility-picker .dropdown');
		// Click the org option (Share Test Org)
		await page.locator('.visibility-picker .option:has-text("Share Test Org")').click();
		// Picker should now show the org name
		await expect(page.locator('.visibility-btn')).toContainText('Share Test Org');

		const noteUrl = page.url();

		// Switch to bob — he should see the note in his sidebar
		await switchUser(page, baseURL, seedUsers.userB.id);
		await expect(
			page.locator('.node-label:has-text("Org Visible Note")'),
			'org member should see the note'
		).toBeVisible({ timeout: 15_000 });

		// Bob can also navigate directly
		await page.goto(noteUrl);
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		await expect(page.locator('.page-title')).toContainText('Org Visible Note');
	});

	test('setting back to Private hides note from org member', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		// Title the note
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		await page.locator('.page-title').click();
		await page.waitForSelector('input.title-edit');
		await page.locator('input.title-edit').fill('Will Be Hidden');
		await page.locator('input.title-edit').press('Enter');

		// Create org, add bob
		await page.goto('/settings/orgs');
		await page.locator('input[placeholder="New organization name…"]').fill('Hidden Test Org');
		await page.keyboard.press('Enter');
		await page.locator('.org-name-btn').first().click();
		await page.waitForSelector('.members-panel h2');
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(2);

		// Navigate to note and set visibility to org
		await page.goto('/');
		await page.waitForSelector('.node-label:has-text("Will Be Hidden")', { timeout: 15_000 });
		await page.locator('.node-label:has-text("Will Be Hidden")').click();
		await page.waitForSelector('.visibility-btn');
		await page.locator('.visibility-btn').click();
		await page.locator('.visibility-picker .option:has-text("Hidden Test Org")').click();
		await expect(page.locator('.visibility-btn')).toContainText('Hidden Test Org');

		// Now set back to Private
		await page.locator('.visibility-btn').click();
		await page.locator('.visibility-picker .option:has-text("Private")').click();
		await expect(page.locator('.visibility-btn')).toContainText('Private');

		const noteUrl = page.url();

		// Switch to bob — should not see the note
		await switchUser(page, baseURL, seedUsers.userB.id);
		await page.goto(noteUrl);
		await expect(page.locator('.not-found')).toBeVisible({ timeout: 10_000 });
	});
});
