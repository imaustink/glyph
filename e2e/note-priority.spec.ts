import { test, expect, createNewPage, navigateToTaskBoard } from './fixtures';

/**
 * Creates a task from a TODO bullet on the currently-open note.
 * Assumes the editor is mounted (a note page is open).
 */
async function createTaskInEditor(page: import('@playwright/test').Page, title: string) {
	const editor = page.locator('main .tiptap-editor');
	await editor.waitFor({ state: 'visible' });
	// A freshly-created page loads its (empty) content asynchronously and calls
	// setContent() once it resolves — typing before that wipes our input. Wait for
	// the network to settle so the initial content load has completed.
	await page.waitForLoadState('networkidle');
	await editor.click();
	await editor.pressSequentially('# TODO', { delay: 30 });
	await editor.press('Enter');
	await editor.pressSequentially(`- ${title}`, { delay: 30 });

	const popover = page.locator('[role="dialog"][aria-label="Create task"]');
	await expect(popover).toBeVisible({ timeout: 5_000 });
	await expect(popover.locator('input.title-input')).toHaveValue(title);
	await popover.locator('button.btn-primary').click();
	await expect(popover).not.toBeVisible();
}

test.describe('Note priority', () => {
	// These tests type into the ProseMirror editor with precise timing;
	// run them serially so CPU contention doesn't cause flaky failures.
	test.describe.configure({ mode: 'serial' });

	test('note priority selector persists across reload', async ({ page }) => {
		await createNewPage(page);

		const prioritySelect = page.locator('.priority-select');
		await expect(prioritySelect).toBeVisible();
		await expect(prioritySelect).toHaveValue('none');

		await prioritySelect.selectOption('high');
		await expect(prioritySelect).toHaveValue('high');

		// Reload — the priority should have been persisted.
		await page.reload();
		const reloaded = page.locator('.priority-select');
		await expect(reloaded).toHaveValue('high', { timeout: 10_000 });
	});

	test('task board sorts by note priority before task priority', async ({ page }) => {
		// Note A (created first) → task "Alpha task"; note priority left as none,
		// but its task will be set to urgent priority.
		await createNewPage(page);
		await createTaskInEditor(page, 'Alpha task');

		// Note B (created second) → task "Bravo task"; note priority set to urgent,
		// task priority left as none.
		await createNewPage(page);
		await createTaskInEditor(page, 'Bravo task');
		await page.locator('.priority-select').selectOption('urgent');
		await expect(page.locator('.priority-select')).toHaveValue('urgent');

		// Raise Alpha task's own priority to urgent via the task detail page.
		await navigateToTaskBoard(page);
		await page.locator('.task-card:has-text("Alpha task")').click();
		await page.waitForURL(/\/tasks\/[^/]+$/, { timeout: 15_000 });
		const priorityMeta = page.locator(
			'.meta-row:has(.meta-label:has-text("Priority")) .meta-select'
		);
		await priorityMeta.selectOption('urgent');
		await expect(priorityMeta).toHaveValue('urgent');

		// Back on the board, the "All Tasks" lane orders by note priority first:
		// Bravo (urgent note) must appear before Alpha (none note, urgent task).
		await navigateToTaskBoard(page);
		const allTasksLane = page.locator('.lane:has(.lane-title:has-text("All Tasks"))');
		await expect(allTasksLane.locator('.task-card:has-text("Alpha task")')).toBeVisible({
			timeout: 5_000
		});
		await expect(allTasksLane.locator('.task-card:has-text("Bravo task")')).toBeVisible();

		const titles = await allTasksLane.locator('.card-title').allInnerTexts();
		const bravoIndex = titles.findIndex((t) => t.includes('Bravo task'));
		const alphaIndex = titles.findIndex((t) => t.includes('Alpha task'));
		expect(bravoIndex).toBeGreaterThanOrEqual(0);
		expect(alphaIndex).toBeGreaterThanOrEqual(0);
		expect(bravoIndex).toBeLessThan(alphaIndex);
	});
});
