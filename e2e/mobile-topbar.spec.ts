import { test, expect } from './fixtures';

/**
 * Phones get an in-flow top bar (menu · brand · search) instead of a floating
 * menu button parked over an empty band at the top of every page (#111).
 */
test.describe('Mobile top bar (phone viewport)', () => {
	// iPhone 17 Pro in portrait.
	test.use({ viewport: { width: 402, height: 874 } });

	test('menu button opens the drawer and the content starts below the bar', async ({ page }) => {
		const topbar = page.locator('.mobile-topbar');
		const sidebar = page.locator('.sidebar-container');

		await expect(topbar).toBeVisible();
		await expect(page.locator('.mobile-menu-btn')).toBeHidden();
		await expect(sidebar).not.toHaveClass(/open/);

		// The page's content follows the bar directly, not a reserved gap.
		const barBox = (await topbar.boundingBox())!;
		const mainBox = (await page.locator('.main-content').boundingBox())!;
		expect(mainBox.y).toBeGreaterThanOrEqual(barBox.y + barBox.height - 1);

		await topbar.getByRole('button', { name: 'Open menu' }).click();
		await expect(sidebar).toHaveClass(/open/);
	});

	test('search button opens the search modal', async ({ page }) => {
		await page.locator('.mobile-topbar').getByRole('button', { name: 'Search' }).click();
		await expect(page.getByPlaceholder('Search pages and tasks…')).toBeVisible();
	});
});
