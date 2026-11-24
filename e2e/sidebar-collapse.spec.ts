import { test, expect } from './fixtures';

/**
 * Sidebar collapse on viewports wider than the mobile drawer breakpoint
 * (768px), e.g. a phone in landscape (#104). The sidebar is a persistent panel
 * there, so its "Toggle sidebar" button must actually hide it, and a button
 * must remain to bring it back.
 */
test.describe('Sidebar collapse (wide viewport)', () => {
	// iPhone 17 Pro in landscape.
	test.use({ viewport: { width: 874, height: 402 } });

	test('toggle button collapses and the menu button reopens the sidebar', async ({ page }) => {
		const sidebar = page.locator('.sidebar');
		const menuBtn = page.locator('.mobile-menu-btn');

		await expect(sidebar).toBeVisible();
		await expect(menuBtn).toBeHidden();

		await page.locator('.sidebar-header button[title="Toggle sidebar"]').click();
		await expect(sidebar).toBeHidden();
		await expect(menuBtn).toBeVisible();

		await menuBtn.click();
		await expect(sidebar).toBeVisible();
		await expect(menuBtn).toBeHidden();
	});
});
