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

	test('pasting a copy of a linked bullet does not share its task (DI-10)', async ({ page }) => {
		const taskId = await createLinkedBullet(page, 'Buy milk');
		const original = page.locator(`main .tiptap-editor li[data-task-id="${taskId}"]`);
		const nodeId = (await original.getAttribute('data-node-id'))!;

		// Paste the bullet (as the editor copies it: with its ids) at the end
		// of the note, below the list.
		await page.locator('main .tiptap-editor').click();
		await page.keyboard.press('Control+End');
		await page.keyboard.press('Enter');
		await page.keyboard.press('Enter');
		await selectionSettled(page);
		await page.locator('main .tiptap-editor').evaluate(
			(el, { nodeId, taskId }) => {
				const data = new DataTransfer();
				data.setData(
					'text/html',
					`<ul><li data-node-id="${nodeId}" data-task-id="${taskId}" data-task-status="todo"><p>Buy milk</p></li></ul>`
				);
				el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
			},
			{ nodeId, taskId }
		);

		const bullets = page.locator('main .tiptap-editor li:has-text("Buy milk")');
		await expect(bullets).toHaveCount(2);
		// Still exactly one bullet linked to the task, and one with its nodeId.
		await expect(original).toHaveCount(1);
		await expect(page.locator(`main .tiptap-editor li[data-node-id="${nodeId}"]`)).toHaveCount(1);
	});

	test('undoing a bullet deletion restores its task, even after a while (DI-10)', async ({ page }) => {
		const taskId = await createLinkedBullet(page, 'Buy milk');
		const bullet = page.locator(`main .tiptap-editor li[data-task-id="${taskId}"]`);

		// Let the editor settle on the linked bullet (its 1 s removal check).
		await page.waitForTimeout(1500);

		// Remove the bullet: Backspace at its start turns it into a paragraph.
		await bullet.locator('p').click();
		await page.keyboard.press('Home');
		await selectionSettled(page);
		await page.keyboard.press('Backspace');
		await expect(bullet).toHaveCount(0);
		// Longer than the 1 s removal debounce.
		await page.waitForTimeout(2500);

		await page.keyboard.press('ControlOrMeta+z');
		await expect(bullet).toHaveCount(1);
		await expect(bullet).toContainText('Buy milk');
		await page.waitForTimeout(1500);

		await bullet.locator('.task-open-link').click();
		await expect(page.locator('h1.task-title')).toHaveText('Buy milk', { timeout: 15_000 });
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
