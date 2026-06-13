/**
 * E2E tests for visibility controls on tasks, folders, and templates (api mode only).
 *
 * Covers:
 * - Task detail page shows VisibilityPicker for the owner
 * - Task visibility can be changed to an org
 * - Folder context menu has "Set visibility…" for the owner
 * - Folder "Set visibility…" opens the visibility modal
 * - Folder "Share with people…" opens ShareDialog
 * - Template list shows VisibilityPicker per template for the owner
 */

import { test, expect, switchUser, navigateToTaskBoard } from './fixtures';

/** Create a task via TODO bullet in the active note editor. */
async function createTaskViaEditor(page: import('@playwright/test').Page, taskTitle: string) {
	const editor = page.locator('main .tiptap-editor');
	await editor.click();
	await editor.pressSequentially('# TODO', { delay: 30 });
	await editor.press('Enter');
	await editor.pressSequentially(`- ${taskTitle}`, { delay: 30 });
	const popover = page.locator('[role="dialog"][aria-label="Create task"]');
	await expect(popover).toBeVisible({ timeout: 10_000 });
	await popover.locator('button.btn-primary').click();
	await expect(popover).not.toBeVisible();
}

// ─── Task visibility ──────────────────────────────────────────────────────────

test.describe('Task visibility (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('task detail shows visibility picker for owner', async ({ page }) => {
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		await createTaskViaEditor(page, 'Visibility Task');
		await navigateToTaskBoard(page);

		await page.locator('.task-card:has-text("Visibility Task")').click({ timeout: 10_000 });
		await page.waitForSelector('.task-detail-page', { timeout: 10_000 });

		await expect(page.locator('.visibility-btn')).toBeVisible({ timeout: 10_000 });
		await expect(page.locator('.visibility-btn')).toContainText('Private');
	});

	test('task visibility picker opens dropdown with Private selected', async ({ page }) => {
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		await createTaskViaEditor(page, 'Dropdown Task');
		await navigateToTaskBoard(page);

		await page.locator('.task-card:has-text("Dropdown Task")').click({ timeout: 10_000 });
		await page.waitForSelector('.task-detail-page', { timeout: 10_000 });

		await page.locator('.visibility-btn').click();
		await expect(page.locator('.visibility-picker .dropdown')).toBeVisible();
		await expect(page.locator('.visibility-picker .option.selected')).toContainText('Private');
	});

	test('task visibility can be set to an org', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		// Create an org and add bob
		await page.goto('/settings/orgs');
		await page.locator('input[placeholder="New organization name…"]').fill('Task Org');
		await page.keyboard.press('Enter');
		await page.locator('.org-name-btn').first().click();
		await page.waitForSelector('.members-panel h2');
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(2);

		// Go to notes and create a task
		await page.goto('/');
		await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
		await createTaskViaEditor(page, 'Org Task');

		await navigateToTaskBoard(page);
		await page.locator('.task-card:has-text("Org Task")').click({ timeout: 10_000 });
		await page.waitForSelector('.task-detail-page', { timeout: 10_000 });

		// Change visibility to the org
		await page.locator('.visibility-btn').click();
		await page.locator('.visibility-picker .option:has-text("Task Org")').click();
		await expect(page.locator('.visibility-btn')).toContainText('Task Org');
	});
});

// ─── Folder visibility ────────────────────────────────────────────────────────

test.describe('Folder visibility (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('folder context menu shows "Set visibility…" for owner', async ({ page }) => {
		// Create a folder (gets default name "New Folder")
		await page.locator('.section-actions button[title="New folder"]').click();
		await expect(page.locator('.node-row.folder')).toBeVisible({ timeout: 10_000 });

		// Right-click to open context menu
		await page.locator('.node-row.folder .node-label').first().click({ button: 'right' });
		await page.waitForSelector('.context-menu');

		await expect(page.locator('.context-menu button:has-text("Set visibility…")')).toBeVisible();
	});

	test('folder "Set visibility…" opens visibility modal with picker', async ({ page }) => {
		await page.locator('.section-actions button[title="New folder"]').click();
		await expect(page.locator('.node-row.folder')).toBeVisible({ timeout: 10_000 });

		await page.locator('.node-row.folder .node-label').first().click({ button: 'right' });
		await page.waitForSelector('.context-menu');
		await page.locator('.context-menu button:has-text("Set visibility…")').click();

		await expect(page.locator('.visibility-modal')).toBeVisible();
		await expect(page.locator('.visibility-modal .visibility-btn')).toContainText('Private');
	});

	test('folder "Share with people…" opens ShareDialog', async ({ page }) => {
		await page.locator('.section-actions button[title="New folder"]').click();
		await expect(page.locator('.node-row.folder')).toBeVisible({ timeout: 10_000 });

		await page.locator('.node-row.folder .node-label').first().click({ button: 'right' });
		await page.waitForSelector('.context-menu');
		await page.locator('.context-menu button:has-text("Share with people…")').click();

		await expect(page.locator('.modal[aria-label="Share"]')).toBeVisible();
	});

	test('folder visibility can be set to an org', async ({ page, seedUsers, baseURL }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		// Create org
		await page.goto('/settings/orgs');
		await page.locator('input[placeholder="New organization name…"]').fill('Folder Org');
		await page.keyboard.press('Enter');
		await page.locator('.org-name-btn').first().click();
		await page.waitForSelector('.members-panel h2');
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(2);

		// Back to sidebar; create a folder
		await page.goto('/');
		await page.waitForSelector('.app-shell .sidebar');
		await page.locator('.section-actions button[title="New folder"]').click();
		await expect(page.locator('.node-row.folder')).toBeVisible({ timeout: 10_000 });

		// Open visibility modal and set org
		await page.locator('.node-row.folder .node-label').first().click({ button: 'right' });
		await page.waitForSelector('.context-menu');
		await page.locator('.context-menu button:has-text("Set visibility…")').click();
		await page.waitForSelector('.visibility-modal');

		await page.locator('.visibility-modal .visibility-btn').click();
		await page.locator('.visibility-picker .option:has-text("Folder Org")').click();
		await expect(page.locator('.visibility-modal .visibility-btn')).toContainText('Folder Org');
	});
});

// ─── Template visibility ──────────────────────────────────────────────────────

test.describe('Template visibility (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	/** Open the template manager modal via sidebar hover → dropdown. */
	async function openTemplateManager(page: import('@playwright/test').Page) {
		// Hover the new-page section actions to reveal the template dropdown trigger
		await page.locator('.section-actions button[title="New page (default template)"]').hover();
		await page.waitForSelector('.template-dropdown', { timeout: 5_000 });
		await page.locator('.dropdown-item.dropdown-manage:has-text("Manage templates")').click();
		await page.waitForSelector('.templates-modal', { timeout: 10_000 });
	}

	test('template list shows visibility picker for owner', async ({ page }) => {
		await openTemplateManager(page);
		// The default template row should show a visibility picker for the owner
		await expect(page.locator('.template-row .visibility-btn').first()).toBeVisible({ timeout: 10_000 });
	});

	test('template visibility picker shows Private by default', async ({ page }) => {
		await openTemplateManager(page);
		await expect(page.locator('.template-row .visibility-btn').first()).toContainText('Private');
	});

	test('template visibility dropdown opens with Private selected', async ({ page }) => {
		await openTemplateManager(page);
		await page.locator('.template-row .visibility-btn').first().click();
		await expect(page.locator('.visibility-picker .dropdown')).toBeVisible();
		await expect(page.locator('.visibility-picker .option.selected')).toContainText('Private');
	});
});
