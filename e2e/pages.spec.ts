import { test, expect, createNewPage, navigateToPage, createNewFolder } from './fixtures';

test.describe('Page management', () => {
	test('app boots with Getting Started page', async ({ page }) => {
		// After reset + navigation, the layout auto-creates "Getting Started".
		await expect(page.locator('.page-title')).toHaveText('Getting Started');
		// Sidebar should show the page.
		await expect(page.locator('.node-label:has-text("Getting Started")')).toBeVisible();
	});

	test('create a new page via sidebar', async ({ page }) => {
		await createNewPage(page);

		// Should navigate to the new (untitled) page.
		const url = page.url();
		expect(url).toContain('/notes/');

		// Sidebar now has two entries: "Getting Started" + the new page.
		const nodes = page.locator('.tree-node .node-label');
		await expect(nodes).toHaveCount(2);
	});

	test('rename a page inline', async ({ page }) => {
		// Click the title to enter edit mode.
		await page.locator('.page-title').click();
		const input = page.locator('input.title-edit');
		await expect(input).toBeVisible();

		// Clear and type a new name.
		await input.fill('My Renamed Page');
		await input.press('Enter');

		// Title should update.
		await expect(page.locator('.page-title')).toHaveText('My Renamed Page');
		// Sidebar should reflect the change.
		await expect(page.locator('.node-label:has-text("My Renamed Page")')).toBeVisible();
	});

	test('navigate between pages', async ({ page }) => {
		// Create a second page — it starts in title-editing mode.
		await createNewPage(page);

		// The title input should already be visible (auto-focused on new page).
		const input = page.locator('input.title-edit');
		await expect(input).toBeVisible({ timeout: 3_000 });
		await input.fill('Second Page');
		await input.press('Enter');
		await expect(page.locator('.page-title')).toHaveText('Second Page');

		// Navigate back to Getting Started.
		await navigateToPage(page, 'Getting Started');
		await expect(page.locator('.page-title')).toHaveText('Getting Started');

		// Navigate forward to Second Page.
		await navigateToPage(page, 'Second Page');
		await expect(page.locator('.page-title')).toHaveText('Second Page');
	});

	test('delete a page via context menu', async ({ page }) => {
		// Create a second page so deleting doesn't leave us with nothing.
		await createNewPage(page);

		// New page starts in title-editing mode.
		const input = page.locator('input.title-edit');
		await expect(input).toBeVisible({ timeout: 3_000 });
		await input.fill('Delete Me');
		await input.press('Enter');

		// Right-click (or click three-dot menu) to open context menu.
		const nodeRow = page.locator('.node-row', {
			has: page.locator('.node-label:has-text("Delete Me")')
		});
		// Hover to show the actions button.
		await nodeRow.hover();
		await nodeRow.locator('.icon-btn[title="More options"]').click();

		// Click "Delete" in the context menu.
		await page.locator('.context-item:has-text("Delete")').click();

		// For pages without tasks, the delete happens immediately (no modal).
		// Wait for the page to be removed from the sidebar.
		await expect(page.locator('.node-label:has-text("Delete Me")')).toHaveCount(0, {
			timeout: 5_000
		});
	});

	test('create a folder', async ({ page }) => {
		await createNewFolder(page);

		// A new folder should appear in the sidebar.
		const folderNode = page.locator('.node-row.folder');
		await expect(folderNode).toBeVisible();
	});

	test('page tags can be added', async ({ page }) => {
		// Click the tags row to open the tag editor.
		const tagsDisplay = page.locator('.tags-display');
		await tagsDisplay.click();

		// The tag input should be visible.
		const tagInput = page.locator('.tags-edit-wrapper input');
		await expect(tagInput).toBeVisible();

		// Type a tag and press Enter.
		await tagInput.fill('important');
		await tagInput.press('Enter');

		// The tag pill should be visible.
		await expect(page.locator('.tag-pill:has-text("important")')).toBeVisible();

		// Close the tag editor.
		await page.locator('button:has-text("Done")').click();
	});
});
