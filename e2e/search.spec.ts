import { test, expect, openSearchModal, typeInEditor, createNewPage } from './fixtures';

test.describe('Search', () => {
	test('open search modal with Cmd+K', async ({ page }) => {
		await openSearchModal(page);
		await expect(page.locator('.search-panel')).toBeVisible();
		await expect(page.locator('.search-input')).toBeFocused();
	});

	test('close search modal with Escape', async ({ page }) => {
		await openSearchModal(page);
		await expect(page.locator('.search-panel')).toBeVisible();

		await page.keyboard.press('Escape');
		await expect(page.locator('.search-panel')).not.toBeVisible();
	});

	test('search finds the Getting Started page', async ({ page }) => {
		await openSearchModal(page);
		await page.locator('.search-input').fill('Getting Started');

		// Wait for results to appear.
		const results = page.locator('.result-item');
		await expect(results.first()).toBeVisible({ timeout: 15_000 });
		await expect(results.first().locator('.result-title')).toContainText('Getting Started');
	});

	test('click search result navigates to page', async ({ page }) => {
		await openSearchModal(page);
		await page.locator('.search-input').fill('Getting Started');

		const result = page.locator('.result-item').first();
		await expect(result).toBeVisible({ timeout: 15_000 });
		await result.click();

		// Modal should close.
		await expect(page.locator('.search-panel')).not.toBeVisible();
		// Should be on the Getting Started page.
		await expect(page.locator('.page-title')).toHaveText('Getting Started');
	});

	test('search with no results shows empty state', async ({ page }) => {
		await openSearchModal(page);
		await page.locator('.search-input').fill('xyznonexistentzyx');

		await expect(page.locator('.no-results')).toBeVisible({ timeout: 15_000 });
	});

	test('search finds a renamed page', async ({ page }) => {
		// Rename the Getting Started page to something unique.
		await page.locator('.page-title').click();
		const input = page.locator('input.title-edit');
		await input.fill('UniqueSearchablePage12345');
		await input.press('Enter');

		// Wait for the store to update.
		await page.waitForTimeout(500);

		// Navigate to the full search page (which rebuilds the index on mount).
		await page.locator('a.nav-item:has-text("Search")').click();
		await page.waitForSelector('.search-input', { timeout: 5_000 });
		await page.locator('.search-input').fill('UniqueSearchablePage');

		const results = page.locator('.result-item');
		await expect(results.first()).toBeVisible({ timeout: 5_000 });
		await expect(results.first().locator('.result-title')).toContainText(
			'UniqueSearchablePage12345'
		);
	});

	test('full search page works', async ({ page }) => {
		// Navigate to the search page.
		await page.locator('a.nav-item:has-text("Search")').click();
		await page.waitForSelector('.search-input', { timeout: 5_000 });

		// Type a query.
		await page.locator('.search-input').fill('Getting Started');

		// Results should appear.
		const results = page.locator('.result-item');
		await expect(results.first()).toBeVisible({ timeout: 3_000 });
	});
});
