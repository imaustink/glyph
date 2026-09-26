/**
 * Data-integrity regressions in the note editor and its note↔task links
 * (docs/data-integrity-audit.md). Each test names the finding it covers.
 */
import { test, expect, navigateToTaskBoard, selectionSettled, waitForEditorReady } from './fixtures';
import type { Page } from '@playwright/test';

const popover = (page: Page) => page.locator('[role="dialog"][aria-label="Create task"]');

/** Type a TODO heading and one bullet, confirm its task, and return the task id. */
async function createLinkedBullet(page: Page, text: string): Promise<string> {
	await waitForEditorReady(page);
	const editor = page.locator('main .tiptap-editor');
	await editor.click();
	await page.keyboard.press('Control+End');
	await editor.press('Enter');
	await editor.pressSequentially('# TODO', { delay: 30 });
	await editor.press('Enter');
	await editor.pressSequentially(`- ${text}`, { delay: 30 });
	await expect(popover(page)).toBeVisible({ timeout: 15_000 });
	await expect(popover(page).locator('input.title-input')).toHaveValue(text, { timeout: 5_000 });
	await popover(page).locator('button.btn-primary').click();
	await expect(popover(page)).not.toBeVisible();
	const li = page.locator(`main .tiptap-editor li[data-task-id]:has-text("${text}")`);
	await expect(li).toHaveCount(1);
	return (await li.getAttribute('data-task-id'))!;
}

test.describe('Editor data integrity', () => {
	test.describe.configure({ mode: 'serial' });

	test('Enter at the start of a linked bullet keeps its task on the text (DI-09)', async ({ page }) => {
		const taskId = await createLinkedBullet(page, 'Buy milk');

		// Put the cursor at the very start of "Buy milk" and press Enter.
		await page.locator('main .tiptap-editor li:has-text("Buy milk") p').click();
		await page.keyboard.press('Home');
		await selectionSettled(page);
		await page.keyboard.press('Enter');

		const milk = page.locator('main .tiptap-editor li:has-text("Buy milk")');
		await expect(milk).toHaveAttribute('data-task-id', taskId);
		// The new empty bullet above it is not linked to anything, and no
		// second "Buy milk" task was created.
		await page.waitForTimeout(1500);
		await expect(page.locator(`main .tiptap-editor li[data-task-id="${taskId}"]`)).toHaveCount(1);
		await expect(popover(page)).not.toBeVisible();

		await navigateToTaskBoard(page);
		await expect(page.locator('.lane').first().locator('.task-card:has-text("Buy milk")')).toHaveCount(1, { timeout: 15_000 });
	});

	test('a task renamed on its page is not reverted by typing in its bullet (DI-29)', async ({ page }) => {
		const taskId = await createLinkedBullet(page, 'Buy milk');
		const bullet = page.locator(`main .tiptap-editor li[data-task-id="${taskId}"]`);

		// Rename the task on its own page.
		await bullet.locator('.task-open-link').click();
		await expect(page.locator('.task-detail-page')).toBeVisible({ timeout: 15_000 });
		await page.locator('h1.task-title').dblclick();
		await page.locator('input.title-edit').fill('Buy oat milk');
		await page.locator('input.title-edit').press('Enter');
		await expect(page.locator('h1.task-title')).toHaveText('Buy oat milk');

		// Back in the note, the bullet shows the new title…
		await page.locator('a.source-link').click();
		await waitForEditorReady(page);
		await expect(bullet).toHaveText('Buy oat milk');

		// …and typing in it keeps the rename.
		await bullet.locator('p').click();
		await page.keyboard.press('End');
		await selectionSettled(page);
		await page.keyboard.type('!');
		await page.waitForTimeout(1500);
		await bullet.locator('.task-open-link').click();
		await expect(page.locator('h1.task-title')).toHaveText('Buy oat milk!', { timeout: 15_000 });
	});
});
