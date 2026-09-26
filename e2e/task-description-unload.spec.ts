import { test, expect } from './fixtures';

/**
 * DI-30: the task description is saved on a 600 ms debounce. A reload or tab
 * close inside that window used to drop the edit.
 */

/** Create a standalone task through the "New task" modal; lands on its detail page. */
async function createTask(page: import('@playwright/test').Page, title: string) {
	await page.locator('button[title="New task"]').click();
	const modal = page.locator('.modal');
	await modal.locator('.title-input').fill(title);
	await modal.locator('.btn-primary').click();
	await expect(page.locator('.task-detail-page')).toBeVisible({ timeout: 15_000 });
	await expect(page.locator('h1.task-title')).toHaveText(title);
}

test.describe('Pending task description on reload [DI-30]', () => {
	test('a description typed just before a reload is saved', async ({ page }) => {
		await createTask(page, 'Unload flush task');

		const description = page.locator('.md-editor');
		await description.click();
		await description.pressSequentially('typed right before reload', { delay: 5 });
		// Reload inside the 600 ms debounce window.
		await page.reload();

		await expect(page.locator('.md-editor')).toContainText('typed right before reload', { timeout: 10_000 });
	});
});
