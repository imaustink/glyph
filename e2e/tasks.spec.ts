import {
	test,
	expect,
	typeInEditor,
	navigateToTaskBoard,
	createNewPage,
	navigateToPage
} from './fixtures';

test.describe('Tasks', () => {
	// Task tests involve typing in the ProseMirror editor with precise timing;
	// run them serially so CPU contention doesn't cause flaky failures.
	test.describe.configure({ mode: 'serial' });

	test('task board shows default lanes', async ({ page }) => {
		await navigateToTaskBoard(page);

		// The lanes store seeds four default lanes when empty.
		const laneTitles = page.locator('.lane-title');
		const count = await laneTitles.count();
		expect(count).toBeGreaterThanOrEqual(4);
	});

	test('empty bullet immediately becomes a smart bullet', async ({ page }) => {
		const editor = page.locator('main .tiptap-editor');
		await editor.click();

		// Type a TODO heading, then create an empty bullet with just "- " (no text).
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		// Typing "- " triggers tiptap's input rule and creates an empty list item.
		await editor.pressSequentially('- ', { delay: 30 });

		// The TaskCreationPopover should appear immediately — before typing any text.
		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });

		// Title input should be empty (no text typed yet).
		const titleInput = popover.locator('input.title-input');
		await expect(titleInput).toHaveValue('');

		// Type the title in the editor and confirm the popover reflects it.
		await editor.pressSequentially('Empty bullet task', { delay: 30 });
		await expect(titleInput).toHaveValue('Empty bullet task', { timeout: 3_000 });

		// Confirm.
		await popover.locator('button.btn-primary').click();
		await expect(popover).not.toBeVisible();

		await navigateToTaskBoard(page);
		await expect(page.locator('.task-card:has-text("Empty bullet task")')).toBeVisible({
			timeout: 5_000
		});
	});

	test('create a task via TODO bullet in editor', async ({ page }) => {		// Navigate to the Getting Started page.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();

		// Type a TODO heading followed by a bullet.
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- Buy groceries', { delay: 30 });

		// The TaskCreationPopover should appear (auto-created task).
		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });

		// The popover title input should be pre-filled with the bullet text.
		const titleInput = popover.locator('input.title-input');
		await expect(titleInput).toHaveValue('Buy groceries');

		// Close the popover.
		await popover.locator('button.btn-primary').click();
		await expect(popover).not.toBeVisible();

		// Navigate to the task board — the task should be there.
		await navigateToTaskBoard(page);
		await expect(page.locator('.task-card:has-text("Buy groceries")')).toBeVisible({
			timeout: 5_000
		});
	});

	test('cycle task status on the board', async ({ page }) => {
		// First create a task via the editor.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- Status test task', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();

		// Go to the task board.
		await navigateToTaskBoard(page);
		const card = page.locator('.task-card:has-text("Status test task")').first();
		await expect(card).toBeVisible({ timeout: 5_000 });

		// Click the status dot to cycle the status.
		const statusButton = card.locator('.status-dot');
		await statusButton.click();

		// The dot should change class (from dot-todo to dot-in-progress).
		// Use .first() since the card may appear in multiple lanes.
		await expect(
			page.locator('.task-card:has-text("Status test task")').first().locator('.dot.dot-in-progress')
		).toBeVisible({ timeout: 5_000 });
	});

	test('navigate to task detail from board', async ({ page }) => {
		// Create a task first.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- Detail test task', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();

		// Go to task board and click the task title to navigate to detail.
		await navigateToTaskBoard(page);
		const titleLink = page.locator('.card-title:has-text("Detail test task")');
		await expect(titleLink).toBeVisible({ timeout: 5_000 });
		await titleLink.click();

		// Should be on the task detail page.
		await expect(page.locator('.task-detail-page')).toBeVisible({ timeout: 5_000 });
		await expect(page.locator('h1.task-title')).toContainText('Detail test task');
	});

	test('edit task details on detail page', async ({ page }) => {
		// Create a task.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- Editable task', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();

		// Navigate to task board → task detail.
		await navigateToTaskBoard(page);
		await page.locator('.card-title:has-text("Editable task")').click({ timeout: 5_000 });
		await expect(page.locator('.task-detail-page')).toBeVisible();

		// Change priority.
		const prioritySelect = page.locator('.meta-row:has(.meta-label:has-text("Priority")) select');
		await prioritySelect.selectOption('high');

		// Add a description.
		const textarea = page.locator('textarea#task-desc');
		await textarea.click();
		await textarea.pressSequentially('This is an important task', { delay: 20 });

		// Wait for debounced save (description debounce is 600ms) and API round-trip.
		await page.waitForTimeout(1_500);

		// Navigate away and back — changes should persist.
		await page.locator('.back-btn').click();
		await page.locator('.card-title:has-text("Editable task")').click({ timeout: 5_000 });

		await expect(
			page.locator('.meta-row:has(.meta-label:has-text("Priority")) select')
		).toHaveValue('high');
		await expect(page.locator('textarea#task-desc')).toHaveValue('This is an important task');
	});

	test('add a new lane to the board', async ({ page }) => {
		await navigateToTaskBoard(page);

		// Count existing lanes.
		const initialCount = await page.locator('.lane').count();

		// Click "Add lane".
		await page.locator('.add-lane-btn').click();

		// A new lane should appear.
		await expect(page.locator('.lane')).toHaveCount(initialCount + 1);
	});

	// ── Drag-and-Drop Tests ─────────────────────────────────────────────

	/**
	 * Helper: creates a task via the TODO-bullet editor flow.
	 * Returns after the task is confirmed.
	 */
	async function createTaskViaBullet(page: import('@playwright/test').Page, title: string) {
		// Navigate to the first page (Getting Started).
		await page.locator('.node-label').first().click();
		await page.waitForSelector('main .tiptap-editor', { timeout: 5_000 });

		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially(`- ${title}`, { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();
		await expect(popover).not.toBeVisible();

		// Press Enter so the next bullet starts on a new line.
		await editor.press('Enter');
	}

	/**
	 * Helper: drag an element to a target using slow mouse moves.
	 * svelte-dnd-action requires realistic pointer events with intermediate moves.
	 */
	async function dragTo(
		page: import('@playwright/test').Page,
		source: import('@playwright/test').Locator,
		target: import('@playwright/test').Locator
	) {
		const srcBox = (await source.boundingBox())!;
		const tgtBox = (await target.boundingBox())!;

		const srcX = srcBox.x + srcBox.width / 2;
		const srcY = srcBox.y + srcBox.height / 2;
		const tgtX = tgtBox.x + tgtBox.width / 2;
		const tgtY = tgtBox.y + tgtBox.height / 2;

		await page.mouse.move(srcX, srcY);
		await page.mouse.down();
		// Move in small steps so DnD library detects motion.
		const steps = 10;
		for (let i = 1; i <= steps; i++) {
			await page.mouse.move(
				srcX + ((tgtX - srcX) * i) / steps,
				srcY + ((tgtY - srcY) * i) / steps,
				{ steps: 2 }
			);
		}
		await page.waitForTimeout(100);
		await page.mouse.up();
		// Allow finalize handler to complete.
		await page.waitForTimeout(300);
	}

	test('drag to reorder tasks within a manual-sort lane', async ({ page }) => {
		// First, type a TODO heading so subsequent bullets become tasks.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');

		// Create two tasks.
		await createTaskViaBullet(page, 'First task');
		await createTaskViaBullet(page, 'Second task');

		await navigateToTaskBoard(page);

		// Configure the "All Tasks" lane to use manual sort.
		const allTasksLane = page.locator('.lane').filter({ has: page.locator('.lane-title:has-text("All Tasks")') });
		await allTasksLane.locator('.icon-btn[title="Configure lane"]').click();

		const modal = page.locator('[role="dialog"][aria-label="Configure lane"]');
		await expect(modal).toBeVisible({ timeout: 5_000 });

		// Select "Manual" sort mode.
		await modal.locator('input[type="radio"][value="manual"]').click();
		await modal.locator('button.btn-primary:has-text("Save")').click();
		await expect(modal).not.toBeVisible();

		// Verify both tasks are visible in the lane.
		const firstCard = allTasksLane.locator('.task-card:has-text("First task")');
		const secondCard = allTasksLane.locator('.task-card:has-text("Second task")');
		await expect(firstCard).toBeVisible({ timeout: 5_000 });
		await expect(secondCard).toBeVisible({ timeout: 5_000 });

		// Get initial order.
		const cardsBefore = allTasksLane.locator('.task-card');
		const firstTitle = await cardsBefore.first().locator('.card-title').innerText();

		// Drag the first card to below the second card.
		await dragTo(page, firstCard, secondCard);

		// Verify the order changed — the originally-first card should no longer be first.
		const cardsAfter = allTasksLane.locator('.task-card');
		const newFirstTitle = await cardsAfter.first().locator('.card-title').innerText();
		expect(newFirstTitle).not.toBe(firstTitle);
	});

	test('drag task between lanes changes status', async ({ page }) => {
		// Create a task (starts with status "todo").
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await createTaskViaBullet(page, 'Move me task');

		await navigateToTaskBoard(page);

		// Find the "In Progress" lane.
		const inProgressLane = page.locator('.lane').filter({ has: page.locator('.lane-title:has-text("In Progress")') });
		const inProgressBody = inProgressLane.locator('.lane-body');

		// The "All Tasks" lane should contain our task.
		const allTasksLane = page.locator('.lane').filter({ has: page.locator('.lane-title:has-text("All Tasks")') });
		const taskCard = allTasksLane.locator('.task-card:has-text("Move me task")');
		await expect(taskCard).toBeVisible({ timeout: 5_000 });

		// Drag the task card into the "In Progress" lane body.
		await dragTo(page, taskCard, inProgressBody);

		// The task should now appear in the "In Progress" lane.
		await expect(
			inProgressLane.locator('.task-card:has-text("Move me task")')
		).toBeVisible({ timeout: 5_000 });

		// Verify the status dot changed to in-progress.
		await expect(
			inProgressLane.locator('.task-card:has-text("Move me task") .dot.dot-in-progress')
		).toBeVisible({ timeout: 5_000 });
	});

	test('dragging a task into a lane that already contains it is a no-op', async ({ page }) => {
		// Create a task — starts with status "todo", so it appears in "All Tasks".
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await createTaskViaBullet(page, 'Duplicate drag task');

		await navigateToTaskBoard(page);

		const allTasksLane = page.locator('.lane').filter({ has: page.locator('.lane-title:has-text("All Tasks")') });
		const inProgressLane = page.locator('.lane').filter({ has: page.locator('.lane-title:has-text("In Progress")') });

		// First move the task to "In Progress" so it now appears in both "All Tasks"
		// (any-status filter) and "In Progress".
		const allTasksCard = allTasksLane.locator('.task-card:has-text("Duplicate drag task")');
		await expect(allTasksCard).toBeVisible({ timeout: 5_000 });
		await dragTo(page, allTasksCard, inProgressLane.locator('.lane-body'));
		await expect(
			inProgressLane.locator('.task-card:has-text("Duplicate drag task")')
		).toBeVisible({ timeout: 5_000 });

		// Record how many cards are in "All Tasks" before the attempted duplicate drop.
		const countBefore = await allTasksLane.locator('.task-card').count();

		// Attempt to drag the "In Progress" copy back into "All Tasks" — the task
		// already lives there, so it should be rejected.
		const inProgressCard = inProgressLane.locator('.task-card:has-text("Duplicate drag task")');
		await dragTo(page, inProgressCard, allTasksLane.locator('.lane-body'));

		// "All Tasks" count must not increase — the duplicate was blocked.
		const countAfter = await allTasksLane.locator('.task-card').count();
		expect(countAfter).toBe(countBefore);

		// The card should still be present in "In Progress" (not removed from source).
		await expect(
			inProgressLane.locator('.task-card:has-text("Duplicate drag task")')
		).toBeVisible({ timeout: 5_000 });
	});

	test('manual sort order persists after page reload', async ({ page }) => {
		// Create a TODO heading and two tasks.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await createTaskViaBullet(page, 'Alpha task');
		await createTaskViaBullet(page, 'Beta task');

		// Wait for debounced task-title writes to settle before leaving the editor.
		await page.waitForTimeout(600);

		await navigateToTaskBoard(page);

		// Verify task titles are fully rendered before proceeding.
		const allTasksLane = page.locator('.lane').filter({ has: page.locator('.lane-title:has-text("All Tasks")') });
		await expect(allTasksLane.locator('.task-card:has-text("Alpha task")')).toBeVisible({ timeout: 5_000 });
		await expect(allTasksLane.locator('.task-card:has-text("Beta task")')).toBeVisible({ timeout: 5_000 });

		// Configure "All Tasks" to manual sort.
		await allTasksLane.locator('.icon-btn[title="Configure lane"]').click();
		const modal = page.locator('[role="dialog"][aria-label="Configure lane"]');
		await expect(modal).toBeVisible({ timeout: 5_000 });
		await modal.locator('input[type="radio"][value="manual"]').click();
		await modal.locator('button.btn-primary:has-text("Save")').click();
		await expect(modal).not.toBeVisible();

		// Wait for tasks to appear.
		const alphaCard = allTasksLane.locator('.task-card:has-text("Alpha task")');
		const betaCard = allTasksLane.locator('.task-card:has-text("Beta task")');
		await expect(alphaCard).toBeVisible({ timeout: 5_000 });
		await expect(betaCard).toBeVisible({ timeout: 5_000 });

		// Drag Alpha below Beta to swap order.
		await dragTo(page, alphaCard, betaCard);

		// Record the new order.
		const cardsAfterDrag = allTasksLane.locator('.task-card .card-title');
		const orderAfterDrag: string[] = [];
		for (let i = 0; i < await cardsAfterDrag.count(); i++) {
			orderAfterDrag.push(await cardsAfterDrag.nth(i).innerText());
		}

		// Reload the page.
		await page.reload({ waitUntil: 'load' });
		await page.waitForSelector('.app-shell .sidebar', { timeout: 15_000 });

		// Navigate back to the task board.
		await navigateToTaskBoard(page);

		// Verify order is preserved after reload.
		const cardsAfterReload = allTasksLane.locator('.task-card .card-title');
		await expect(cardsAfterReload).toHaveCount(orderAfterDrag.length, { timeout: 5_000 });
		for (let i = 0; i < orderAfterDrag.length; i++) {
			await expect(cardsAfterReload.nth(i)).toHaveText(orderAfterDrag[i]);
		}
	});

	// ── Delete Task Tests ───────────────────────────────────────────────

	test('delete task from detail page', async ({ page }) => {
		// Create a task.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- Delete me task', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();

		// Navigate to task board → task detail.
		await navigateToTaskBoard(page);
		await page.locator('.card-title:has-text("Delete me task")').click({ timeout: 5_000 });
		await expect(page.locator('.task-detail-page')).toBeVisible();

		// Click the delete button.
		await page.locator('.delete-btn').click();

		// Confirmation modal should appear with the warning about removing the bullet.
		const modal = page.locator('.delete-modal');
		await expect(modal).toBeVisible({ timeout: 3_000 });
		await expect(modal.locator('.delete-modal-warning')).toBeVisible();
		await expect(modal.locator('.delete-modal-warning')).toContainText('bullet');

		// Confirm deletion.
		await modal.locator('.delete-confirm-btn').click();

		// Should navigate back to task board.
		await expect(page.locator('.board-page')).toBeVisible({ timeout: 5_000 });

		// Task should no longer exist on the board.
		await expect(page.locator('.task-card:has-text("Delete me task")')).not.toBeVisible();
	});

	test('delete task removes linked bullet from note', async ({ page }) => {
		// Create a task.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- Linked bullet task', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();

		// Wait for debounced save.
		await page.waitForTimeout(1_500);

		// Navigate to task board → task detail → delete.
		await navigateToTaskBoard(page);
		await page.locator('.card-title:has-text("Linked bullet task")').click({ timeout: 5_000 });
		await page.locator('.delete-btn').click();
		const modal = page.locator('.delete-modal');
		await expect(modal).toBeVisible({ timeout: 3_000 });
		await modal.locator('.delete-confirm-btn').click();
		await expect(page.locator('.board-page')).toBeVisible({ timeout: 5_000 });

		// Navigate back to the note — the bullet should be gone.
		await page.locator('.node-label').first().click();
		await page.waitForSelector('main .tiptap-editor', { timeout: 5_000 });
		await page.waitForTimeout(500);

		const editorText = await page.locator('main .tiptap-editor').innerText();
		expect(editorText).not.toContain('Linked bullet task');
	});

	test('cancel delete does not remove task', async ({ page }) => {
		// Create a task.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- Keep me task', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();

		// Navigate to task detail and click delete.
		await navigateToTaskBoard(page);
		await page.locator('.card-title:has-text("Keep me task")').click({ timeout: 5_000 });
		await page.locator('.delete-btn').click();

		// Cancel the deletion.
		const modal = page.locator('.delete-modal');
		await expect(modal).toBeVisible({ timeout: 3_000 });
		await modal.locator('button.btn-ghost').click();
		await expect(modal).not.toBeVisible();

		// Task should still be visible on the detail page.
		await expect(page.locator('h1.task-title')).toContainText('Keep me task');

		// And on the board.
		await navigateToTaskBoard(page);
		await expect(page.locator('.task-card:has-text("Keep me task")')).toBeVisible({ timeout: 5_000 });
	});

	test('removing a TODO bullet in the editor deletes the task', async ({ page }) => {
		// Create a TODO heading and a task bullet.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- Auto delete task', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();

		// Verify task exists on the board.
		await navigateToTaskBoard(page);
		await expect(page.locator('.task-card:has-text("Auto delete task")')).toBeVisible({
			timeout: 5_000
		});

		// Navigate back to the note.
		await page.locator('.node-label').first().click();
		await page.waitForSelector('main .tiptap-editor', { timeout: 5_000 });

		// Select the entire bullet line and delete it.
		// Click on the bullet text, select all content on that line, then delete.
		const bulletItem = page.locator('main .tiptap-editor li:has-text("Auto delete task")');
		await expect(bulletItem).toBeVisible({ timeout: 5_000 });
		await bulletItem.click();

		// Select all text in the bullet and delete, then backspace to remove the list item.
		await page.keyboard.press('Home');
		await page.keyboard.press('Shift+End');
		await page.keyboard.press('Backspace');
		await page.keyboard.press('Backspace');

		// Wait for debounced save and task deletion.
		await page.waitForTimeout(1_500);

		// Navigate to the task board — the task should be gone.
		await navigateToTaskBoard(page);
		await expect(page.locator('.task-card:has-text("Auto delete task")')).not.toBeVisible({
			timeout: 5_000
		});
	});

	// ── Lane Default Tests ──────────────────────────────────────────────

	test('default lanes include All Tasks, In Progress, Done, and Cancelled', async ({ page }) => {
		await navigateToTaskBoard(page);

		const expectedTitles = ['All Tasks', 'In Progress', 'Done', 'Cancelled'];
		for (const title of expectedTitles) {
			await expect(
				page.locator('.lane-title', { hasText: title })
			).toBeVisible({ timeout: 5_000 });
		}
	});

	test('All Tasks lane shows all tasks via empty filter rules', async ({ page }) => {
		// Create tasks with different statuses.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- All Tasks filter task', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();

		await navigateToTaskBoard(page);

		// The "All Tasks" lane should show the newly created task.
		const allTasksLane = page.locator('.lane').filter({
			has: page.locator('.lane-title:has-text("All Tasks")')
		});
		await expect(allTasksLane.locator('.task-card:has-text("All Tasks filter task")')).toBeVisible({
			timeout: 5_000
		});

		// Verify the lane config has no filter rules (empty rules = show all tasks).
		await allTasksLane.locator('.icon-btn[title="Configure lane"]').click();
		const modal = page.locator('[role="dialog"][aria-label="Configure lane"]');
		await expect(modal).toBeVisible({ timeout: 5_000 });
		await expect(modal.locator('.rule-row')).toHaveCount(0);
		await modal.locator('button.btn-ghost:has-text("Cancel")').click();
	});

	// ── Status-based auto-sort tests ───────────────────────────────────

	test('auto sort sinks done tasks below active tasks', async ({ page }) => {
		// Create two tasks via the editor.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await createTaskViaBullet(page, 'Active sort task');
		await createTaskViaBullet(page, 'Done sort task');

		await navigateToTaskBoard(page);

		const allTasksLane = page.locator('.lane').filter({
			has: page.locator('.lane-title:has-text("All Tasks")')
		});

		// Ensure the lane is in auto sort mode.
		await allTasksLane.locator('.icon-btn[title="Configure lane"]').click();
		const modal = page.locator('[role="dialog"][aria-label="Configure lane"]');
		await expect(modal).toBeVisible({ timeout: 5_000 });
		await modal.locator('input[type="radio"][value="auto"]').click();
		await modal.locator('button.btn-primary:has-text("Save")').click();
		await expect(modal).not.toBeVisible();

		// Both tasks must be visible.
		await expect(allTasksLane.locator('.task-card:has-text("Active sort task")')).toBeVisible({ timeout: 5_000 });
		await expect(allTasksLane.locator('.task-card:has-text("Done sort task")')).toBeVisible({ timeout: 5_000 });

		// Navigate to the "Done sort task" detail page and mark it done.
		await page.locator('.card-title:has-text("Done sort task")').click({ timeout: 5_000 });
		await expect(page.locator('.task-detail-page')).toBeVisible();
		const statusSelect = page.locator('.meta-row:has(.meta-label:has-text("Status")) select');
		await statusSelect.selectOption('done');
		await page.waitForTimeout(500);
		await page.locator('.back-btn').click();

		await expect(page.locator('.board-page')).toBeVisible({ timeout: 5_000 });

		// Wait for the lane to finish (re)rendering its cards after navigating back
		// from the task detail page — the lane sorts tasks via an async $effect, so
		// the cards are not present the instant .board-page becomes visible.
		await expect(
			allTasksLane.locator('.task-card:has-text("Active sort task")')
		).toBeVisible({ timeout: 5_000 });
		await expect(
			allTasksLane.locator('.task-card:has-text("Done sort task")')
		).toBeVisible({ timeout: 5_000 });

		// The "Done sort task" must appear after "Active sort task" in All Tasks.
		const cards = allTasksLane.locator('.task-card .card-title');
		const count = await cards.count();
		let activeIndex = -1;
		let doneIndex = -1;
		for (let i = 0; i < count; i++) {
			const text = await cards.nth(i).innerText();
			if (text.includes('Active sort task')) activeIndex = i;
			if (text.includes('Done sort task')) doneIndex = i;
		}
		expect(activeIndex).toBeGreaterThanOrEqual(0);
		expect(doneIndex).toBeGreaterThanOrEqual(0);
		expect(doneIndex).toBeGreaterThan(activeIndex);
	});

	test('auto sort sinks cancelled tasks below active tasks', async ({ page }) => {
		// Create two tasks via the editor.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await createTaskViaBullet(page, 'Active cancelled sort task');
		await createTaskViaBullet(page, 'Cancelled sort task');

		await navigateToTaskBoard(page);

		const allTasksLane = page.locator('.lane').filter({
			has: page.locator('.lane-title:has-text("All Tasks")')
		});

		// Ensure the lane is in auto sort mode.
		await allTasksLane.locator('.icon-btn[title="Configure lane"]').click();
		const modal = page.locator('[role="dialog"][aria-label="Configure lane"]');
		await expect(modal).toBeVisible({ timeout: 5_000 });
		await modal.locator('input[type="radio"][value="auto"]').click();
		await modal.locator('button.btn-primary:has-text("Save")').click();
		await expect(modal).not.toBeVisible();

		// Navigate to the "Cancelled sort task" detail page and mark it cancelled.
		await page.locator('.card-title').filter({ hasText: /^Cancelled sort task$/ }).click({ timeout: 5_000 });
		await expect(page.locator('.task-detail-page')).toBeVisible();
		const statusSelect = page.locator('.meta-row:has(.meta-label:has-text("Status")) select');
		await statusSelect.selectOption('cancelled');
		await page.waitForTimeout(500);
		await page.locator('.back-btn').click();

		await expect(page.locator('.board-page')).toBeVisible({ timeout: 5_000 });

		// Wait for the lane to finish (re)rendering its cards after navigating back
		// from the task detail page — the lane sorts tasks via an async $effect, so
		// the cards are not present the instant .board-page becomes visible.
		await expect(
			allTasksLane.locator('.card-title', { hasText: 'Active cancelled sort task' })
		).toBeVisible({ timeout: 5_000 });
		await expect(
			allTasksLane.locator('.card-title').filter({ hasText: /^Cancelled sort task$/ })
		).toBeVisible({ timeout: 5_000 });

		// The "Cancelled sort task" must appear after "Active cancelled sort task".
		const cards = allTasksLane.locator('.task-card .card-title');
		const count = await cards.count();
		let activeIndex = -1;
		let cancelledIndex = -1;
		for (let i = 0; i < count; i++) {
			const text = await cards.nth(i).innerText();
			if (text.includes('Active cancelled sort task')) activeIndex = i;
			if (text.includes('Cancelled sort task')) cancelledIndex = i;
		}
		expect(activeIndex).toBeGreaterThanOrEqual(0);
		expect(cancelledIndex).toBeGreaterThanOrEqual(0);
		expect(cancelledIndex).toBeGreaterThan(activeIndex);
	});

	test('new lane shows all tasks by default (empty filter = show all)', async ({ page }) => {
		// Create a task first so the board is non-empty.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- New lane test task', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();

		await navigateToTaskBoard(page);

		// Add a new lane.
		const initialCount = await page.locator('.lane').count();
		await page.locator('.add-lane-btn').click();
		await expect(page.locator('.lane')).toHaveCount(initialCount + 1, { timeout: 5_000 });

		// The new lane (empty filter rules) should show all tasks (same as "All Tasks" lane).
		const newLane = page.locator('.lane').last();
		await expect(newLane.locator('.task-card')).toHaveCount(
			await page.locator('.lane').first().locator('.task-card').count()
		);
	});

	test('task creation popover exposes a URL field', async ({ page, storageMode }) => {
		// The URL/unfurl input only renders in API mode (needs the unfurl endpoint).
		test.skip(storageMode !== 'api', 'Link unfurl only available in API mode');

		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- Task with URL', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });

		// Reveal the optional fields.
		await popover.locator('.expand-btn').click();

		const linkInput = popover.locator('.link-input');
		await expect(linkInput).toBeVisible();

		// Invalid URLs are rejected with an inline error.
		await linkInput.fill('not a valid url');
		await linkInput.press('Enter');
		await expect(popover.locator('.link-error')).toBeVisible({ timeout: 3_000 });

		await popover.locator('button.btn-primary').click();
		await expect(popover).not.toBeVisible();
	});

	test('hover preview stays open when moving the pointer onto it', async ({ page }) => {
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- Hover preview task', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();
		await expect(popover).not.toBeVisible();

		// Hover the linked bullet to trigger the floating preview.
		const bullet = page.locator('main .tiptap-editor li[data-task-id]', {
			hasText: 'Hover preview task'
		});
		await bullet.hover();

		const preview = page.locator('.preview[role="tooltip"]');
		await expect(preview).toBeVisible({ timeout: 5_000 });

		// Move the pointer onto the preview — it must stay visible so "Open" is clickable.
		const openLink = preview.locator('.preview-link');
		await openLink.hover();
		await expect(preview).toBeVisible();
		await expect(openLink).toBeVisible();

		// Clicking "Open" navigates to the task detail page.
		await openLink.click();
		await expect(page.locator('.task-detail-page')).toBeVisible({ timeout: 5_000 });
	});
});
