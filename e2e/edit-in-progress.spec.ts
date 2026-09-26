import { test, expect } from './fixtures';

/**
 * Inputs on the note and task pages must not be reset while the user is
 * editing them just because the record changed for another reason (DI-29).
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

test.describe('Edits in progress [DI-29]', () => {
	test('note tags being edited survive a priority change', async ({ page }) => {
		await page.locator('.tags-display').click();
		const tagInput = page.locator('.tags-edit-wrapper .tag-text-input');
		await tagInput.fill('keepme');
		await tagInput.press('Enter');
		await expect(page.locator('.tags-edit-wrapper .tag-pill:has-text("keepme")')).toBeVisible();

		// Any change to the note record used to re-sync the tag editor from the
		// store, dropping the tag that hadn't been committed yet.
		await page.locator('.priority-select').selectOption('high');
		await expect(page.locator('.tags-edit-wrapper .tag-pill:has-text("keepme")')).toBeVisible();

		await page.locator('.tags-edit-wrapper button:has-text("Done")').click();
		await expect(page.locator('.tags-display .tag-pill:has-text("keepme")')).toBeVisible();

		await page.reload();
		await expect(page.locator('.tags-display .tag-pill:has-text("keepme")')).toBeVisible({ timeout: 10_000 });
	});

	test('a task title being edited survives the description save landing', async ({ page }) => {
		await createTask(page, 'Original title');

		const description = page.locator('.md-editor');
		await description.click();
		await description.pressSequentially('some notes', { delay: 10 });

		// Start renaming before the description's debounced save (600 ms) fires.
		await page.locator('h1.task-title').dblclick();
		const titleInput = page.locator('input.title-edit');
		await titleInput.fill('Renamed while saving');
		// Let the description save go out and come back.
		await page.waitForTimeout(1_500);
		await expect(titleInput).toHaveValue('Renamed while saving');
		await titleInput.press('Enter');

		await expect(page.locator('h1.task-title')).toHaveText('Renamed while saving');
		await page.reload();
		await expect(page.locator('h1.task-title')).toHaveText('Renamed while saving', { timeout: 10_000 });
	});
});
