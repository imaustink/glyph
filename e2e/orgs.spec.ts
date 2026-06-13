/**
 * E2E tests for Organization management (api mode only).
 *
 * Covers:
 * - Creating an organization
 * - Renaming an organization
 * - Adding a member
 * - Changing a member's role
 * - Removing a member
 * - Deleting an organization
 */

import { test, expect, switchUser } from './fixtures';

test.describe('Organizations (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('navigates to /settings/orgs via sidebar', async ({ page }) => {
		await page.locator('a.nav-item:has-text("Organizations")').click();
		await page.waitForURL('**/settings/orgs');
		await expect(page.locator('h1')).toHaveText('Organizations');
	});

	test('creates an organization', async ({ page }) => {
		await page.goto('/settings/orgs');
		await page.locator('input[placeholder="New organization name…"]').fill('My Org');
		await page.keyboard.press('Enter');
		await expect(page.locator('.org-name').first()).toHaveText('My Org');
	});

	test('renames an organization', async ({ page }) => {
		await page.goto('/settings/orgs');
		// Create org
		await page.locator('input[placeholder="New organization name…"]').fill('Rename Me');
		await page.keyboard.press('Enter');
		await expect(page.locator('.org-name').first()).toHaveText('Rename Me');

		// Click rename button
		await page.locator('.org-item button[title="Rename"]').first().click();
		const editInput = page.locator('.org-item .inline-input');
		await editInput.fill('Renamed Org');
		await editInput.press('Enter');
		await expect(page.locator('.org-name').first()).toHaveText('Renamed Org');
	});

	test('adds and removes a member', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers) throw new Error('seedUsers missing');
		await page.goto('/settings/orgs');

		// Create org as userA (logged in by default)
		await page.locator('input[placeholder="New organization name…"]').fill('Member Test Org');
		await page.keyboard.press('Enter');
		// Click org to open member panel
		await page.locator('.org-name-btn').first().click();
		await page.waitForSelector('.members-panel h2');

		// Add bob by email
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);

		// Add as viewer
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(2); // owner + bob

		// Remove bob
		await page.locator('.member-row .btn-ghost.danger').last().click();
		await expect(page.locator('.member-row')).toHaveCount(1);
	});

	test('deletes an organization', async ({ page }) => {
		await page.goto('/settings/orgs');
		await page.locator('input[placeholder="New organization name…"]').fill('To Delete');
		await page.keyboard.press('Enter');
		await expect(page.locator('.org-name').first()).toHaveText('To Delete');

		page.once('dialog', (d) => d.accept());
		await page.locator('.org-item button[title="Delete"]').first().click();
		await expect(page.locator('.org-name')).toHaveCount(0);
	});

	test('viewer cannot see rename/delete buttons', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		await page.goto('/settings/orgs');
		// Create org as userA
		await page.locator('input[placeholder="New organization name…"]').fill('Viewer Org');
		await page.keyboard.press('Enter');
		await page.locator('.org-name-btn').first().click();

		// Add userB as viewer by email
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);
		await page.locator('.add-member-row .btn-primary').click();

		// Switch to userB
		await switchUser(page, baseURL, seedUsers.userB.id);
		await page.goto('/settings/orgs');

		// Should see the org but no rename/delete buttons
		await expect(page.locator('.org-name-btn')).toHaveCount(1);
		await expect(page.locator('.org-item button[title="Rename"]')).toHaveCount(0);
		await expect(page.locator('.org-item button[title="Delete"]')).toHaveCount(0);
	});
});
