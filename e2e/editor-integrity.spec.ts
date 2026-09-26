/**
 * Data-integrity regressions in the note editor and its note↔task links
 * (docs/data-integrity-audit.md). Each test names the finding it covers.
 */
import { test, expect, createNewPage, navigateToTaskBoard, selectionSettled, waitForEditorReady } from './fixtures';
import type { Page } from '@playwright/test';

const popover = (page: Page) => page.locator('[role="dialog"][aria-label="Create task"]');

/**
 * On a fresh, empty note, type a TODO heading and one bullet, confirm its
 * task, and return the task id. A fresh note keeps the tests independent: in
 * the api project the landing note persists between tests (serial mode), and
 * where a click lands in leftover content decides where typing goes.
 */
async function createLinkedBullet(page: Page, text: string): Promise<string> {
	await createNewPage(page);
	const titleInput = page.locator('input.title-edit');
	if (await titleInput.isVisible()) await titleInput.press('Escape');
	const editor = page.locator('main .tiptap-editor');
	await editor.click();
	// The default template may seed content; start from an empty document.
	await page.keyboard.press('ControlOrMeta+a');
	await page.keyboard.press('Backspace');
	await selectionSettled(page);
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

	test('a note with content the editor cannot show opens read-only and is never saved (DI-01)', async ({ page, storageMode }) => {
		test.skip(storageMode !== 'local', 'seeds the stored document directly in localStorage');
		const pageId = new URL(page.url()).pathname.split('/').pop()!;
		const key = `glyph:content:${pageId}`;
		// Let the landing page's own saves (e.g. nodeId assignment) land first,
		// so they can't overwrite the document seeded below.
		await page.waitForTimeout(1000);
		// An image node: accepted by the API (and produced by MCP markdown), but
		// not part of the editor schema.
		await page.evaluate((key) => {
			localStorage.setItem(
				key,
				JSON.stringify({
					content: {
						type: 'doc',
						content: [
							{ type: 'paragraph', content: [{ type: 'text', text: 'Keep me' }] },
							{ type: 'image', attrs: { src: 'https://example.com/a.png', alt: 'a' } }
						]
					},
					updatedAt: new Date().toISOString(),
					schemaVersion: 1
				})
			);
		}, key);
		const before = await page.evaluate((key) => localStorage.getItem(key), key);

		await page.reload();
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });

		await expect(page.locator('.editor-wrapper [role="alert"]')).toContainText(/can.t be shown|read-only/i, { timeout: 15_000 });
		await expect(page.locator('main .tiptap-editor')).toHaveAttribute('contenteditable', 'false');

		// Typing must not reach the stored document.
		await page.locator('main .tiptap-editor').click({ force: true });
		await page.keyboard.type('oops');
		await page.waitForTimeout(1500);
		expect(await page.evaluate((key) => localStorage.getItem(key), key)).toBe(before);
	});

	test('closing the tab right after typing keeps the last edits (DI-30)', async ({ page, storageMode }) => {
		test.skip(storageMode !== 'local', 'a save sent during unload is only observable reliably in local mode');
		await waitForEditorReady(page);
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await page.keyboard.press('Control+End');
		await page.keyboard.press('Enter');
		// Type, then leave well inside the 500 ms save debounce.
		await page.keyboard.type('last words');
		await page.reload();

		await waitForEditorReady(page);
		await expect(page.locator('main .tiptap-editor')).toContainText('last words');
	});

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

	test('undoing a bullet deletion restores its task, even after a while (DI-10)', async ({ page, storageMode }) => {
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

		// TipTap binds "Mod" from navigator.platform, which headless Chromium on
		// macOS doesn't report as a Mac — so derive the key the same way
		// (ControlOrMeta would pick the host's key). Collab's UndoManager can
		// group steps, so undo until the bullet is back.
		const mod = (await page.evaluate(() => /Mac|iPhone|iPad|iPod/.test(navigator.platform))) ? 'Meta' : 'Control';
		await expect(async () => {
			await page.keyboard.press(`${mod}+z`);
			await expect(bullet).toHaveCount(1, { timeout: 1000 });
		}).toPass({ timeout: 15_000 });
		await expect(bullet).toContainText('Buy milk');
		if (storageMode === 'api') {
			// The server restores the task when the restored bullet reaches it —
			// in collab mode, on the next (debounced) snapshot.
			await expect.poll(async () => (await page.request.get(`/api/v1/tasks/${taskId}`)).status(), { timeout: 20_000 }).toBe(200);
		} else {
			await page.waitForTimeout(1500);
		}

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

	// Review follow-up to DI-10: moving a linked bullet to another note used
	// to leave its task deleted with the first note and give the second a
	// bare new task — losing status, priority, due date, description…
	test('cutting a linked bullet from one note and pasting it into another moves its task (DI-10)', async ({ page, storageMode }) => {
		const taskId = await createLinkedBullet(page, 'Pack the tent');
		const noteA = page.url();

		// Give the task some metadata on its own page.
		await page.locator(`main .tiptap-editor li[data-task-id="${taskId}"] .task-open-link`).click();
		await expect(page.locator('.task-detail-page')).toBeVisible({ timeout: 15_000 });
		const meta = (label: string) => page.locator(`.meta-row:has(.meta-label:text-is("${label}")) select`);
		await meta('Status').selectOption('in-progress');
		await meta('Priority').selectOption('high');
		await expect(meta('Status')).toHaveValue('in-progress');
		await expect(meta('Priority')).toHaveValue('high');
		await page.waitForTimeout(500);
		await page.locator('a.source-link').click();
		await waitForEditorReady(page);
		const inA = page.locator(`main .tiptap-editor li[data-task-id="${taskId}"]`);
		await expect(inA).toHaveAttribute('data-task-status', 'in-progress', { timeout: 15_000 });

		// Cut the bullet, as the browser does: a cut event whose clipboard the
		// editor fills, then deletes the selection.
		const html = await page.locator('main .tiptap-editor').evaluate((el, taskId) => {
			type PMNode = { type: { name: string }; attrs: Record<string, unknown> };
			const editor = (el as unknown as {
				editor: {
					state: { doc: { descendants(f: (n: PMNode, pos: number) => void): void } };
					commands: { setNodeSelection(pos: number): boolean };
				};
			}).editor;
			let pos = -1;
			editor.state.doc.descendants((n, p) => {
				if (n.type.name === 'listItem' && n.attrs.taskId === taskId) pos = p;
			});
			editor.commands.setNodeSelection(pos);
			const data = new DataTransfer();
			el.dispatchEvent(new ClipboardEvent('cut', { clipboardData: data, bubbles: true, cancelable: true }));
			return data.getData('text/html');
		}, taskId);
		expect(html).toContain(taskId);
		await expect(inA).toHaveCount(0);

		// Paste it into a new note, under the TODO heading its template has.
		await createNewPage(page);
		const noteB = page.url();
		expect(noteB).not.toBe(noteA);
		const titleInput = page.locator('input.title-edit');
		if (await titleInput.isVisible()) await titleInput.press('Escape');
		const editor = page.locator('main .tiptap-editor');
		await editor.locator('h1:has-text("TODO")').click();
		await page.keyboard.press('End');
		await page.keyboard.press('Enter');
		await selectionSettled(page);
		await editor.evaluate((el, html) => {
			const data = new DataTransfer();
			data.setData('text/html', html);
			el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
		}, html);

		// The pasted bullet is linked to the same task, status and all — in
		// API mode once the server has seen the cut (a collaborative note
		// saves it a moment after it happens).
		const inB = page.locator(`main .tiptap-editor li[data-task-id="${taskId}"]`);
		await expect(inB).toHaveCount(1, { timeout: 25_000 });
		await expect(inB).toContainText('Pack the tent');
		await expect(inB).toHaveAttribute('data-task-status', 'in-progress');
		await expect(popover(page)).not.toBeVisible();

		await inB.locator('.task-open-link').click();
		await expect(page.locator('.task-detail-page')).toBeVisible({ timeout: 15_000 });
		await expect(page).toHaveURL(new RegExp(taskId));
		await expect(meta('Status')).toHaveValue('in-progress');
		await expect(meta('Priority')).toHaveValue('high');
		await page.locator('a.source-link').click();
		await expect(page).toHaveURL(noteB, { timeout: 15_000 });
		await waitForEditorReady(page);

		// One task, not two.
		if (storageMode === 'api') {
			const all = (await (await page.request.get('/api/v1/tasks')).json()) as { id: string; title: string }[];
			expect(all.filter((t) => t.title === 'Pack the tent').map((t) => t.id)).toEqual([taskId]);
		}
		await navigateToTaskBoard(page);
		// The first lane is "All Tasks".
		await expect(page.locator('.lane').first().locator('.task-card:has-text("Pack the tent")')).toHaveCount(1, { timeout: 15_000 });
	});
});

