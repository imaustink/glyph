import { test, expect } from './fixtures';

/**
 * Regression coverage for issue #56: every network request triggered by a
 * user action must surface its failure to the user (rather than failing
 * silently). These tests force the API to reject a write and assert that an
 * error toast appears.
 *
 * Network errors only happen in API mode — local mode writes to localStorage
 * and never hits the network — so each test is skipped for the local project.
 */
test.describe('network errors from user actions are shown', () => {
	test('creating a page shows an error toast when the API fails', async ({ page, storageMode }) => {
		test.skip(storageMode !== 'api', 'network errors only occur in API mode');

		// Fail the page-create request. The initial "Getting Started" page is
		// created during boot (before this route is installed), so this only
		// affects the create triggered by the click below.
		await page.route('**/api/v1/pages', async (route) => {
			if (route.request().method() === 'POST') {
				await route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'boom' })
				});
			} else {
				await route.continue();
			}
		});

		await page.locator('.section-actions button[title="New page (default template)"]').click();

		const toast = page.locator('.toast.toast-error');
		await expect(toast).toBeVisible();
		await expect(toast).toContainText('Failed to create a page.');
	});

	test('creating a folder shows an error toast when the API fails', async ({ page, storageMode }) => {
		test.skip(storageMode !== 'api', 'network errors only occur in API mode');

		await page.route('**/api/v1/pages', async (route) => {
			if (route.request().method() === 'POST') {
				await route.fulfill({
					status: 500,
					contentType: 'application/json',
					body: JSON.stringify({ error: 'boom' })
				});
			} else {
				await route.continue();
			}
		});

		await page.locator('.section-actions button[title="New folder"]').click();

		const toast = page.locator('.toast.toast-error');
		await expect(toast).toBeVisible();
		await expect(toast).toContainText('Failed to create a folder.');
	});
});
