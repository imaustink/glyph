import { test, expect, createNewPage, createNewFolder } from './fixtures';

/**
 * Open the template manager modal via the sidebar's template dropdown.
 * Hovers the "New page" button to reveal the dropdown, then clicks "Manage templates".
 */
async function openTemplateManager(page: import('@playwright/test').Page) {
	const wrapper = page.locator('.new-page-btn-wrapper');
	await wrapper.hover();
	const dropdown = page.locator('.template-dropdown');
	await expect(dropdown).toBeVisible({ timeout: 3_000 });
	await dropdown.locator('.dropdown-manage:has-text("Manage templates")').click();
	await expect(page.locator('.templates-modal')).toBeVisible({ timeout: 3_000 });
}

test.describe('Template default folder', () => {
	test('template manager shows folder picker defaulting to Root', async ({ page }) => {
		await openTemplateManager(page);

		// Click Edit on the Default template.
		const defaultRow = page.locator('.template-row').first();
		await defaultRow.locator('.action-btn:has-text("Edit")').click();

		// The folder select should be visible and show "Root (no folder)".
		const folderSelect = page.locator('#template-default-folder');
		await expect(folderSelect).toBeVisible();
		await expect(folderSelect).toContainText('Root (no folder)');
	});

	test('folder picker lists existing folders', async ({ page }) => {
		// Create a folder first.
		await createNewFolder(page);
		// Rename it.
		const folderInput = page.locator('.node-row.folder input.folder-rename');
		if (await folderInput.isVisible({ timeout: 1_000 }).catch(() => false)) {
			await folderInput.fill('Work Notes');
			await folderInput.press('Enter');
		}

		// Now open template manager and edit the default template.
		await openTemplateManager(page);
		const defaultRow = page.locator('.template-row').first();
		await defaultRow.locator('.action-btn:has-text("Edit")').click();

		// The folder select should list our "Work Notes" folder (or "Untitled folder").
		const folderSelect = page.locator('#template-default-folder');
		await expect(folderSelect).toBeVisible();
		const options = folderSelect.locator('option');
		// At least "Root (no folder)" + one folder.
		const count = await options.count();
		expect(count).toBeGreaterThanOrEqual(2);
	});

	test('setting a default folder creates page inside that folder', async ({ page }) => {
		// 1. Create a folder.
		await createNewFolder(page);
		// Wait for the folder to appear in the sidebar.
		const folderRow = page.locator('.node-row.folder').first();
		await expect(folderRow).toBeVisible();

		// 2. Open template manager → edit default template → set folder.
		await openTemplateManager(page);
		const defaultRow = page.locator('.template-row').first();
		await defaultRow.locator('.action-btn:has-text("Edit")').click();

		const folderSelect = page.locator('#template-default-folder');
		await expect(folderSelect).toBeVisible();

		// Select the second option (first folder — index 0 is "Root").
		await folderSelect.selectOption({ index: 1 });

		// Save.
		await page.locator('.modal-footer .btn-primary:has-text("Save")').click();
		// Wait for list view.
		await expect(page.locator('.templates-list')).toBeVisible({ timeout: 3_000 });

		// Close the modal.
		await page.locator('.modal-footer .btn-ghost:has-text("Close")').click();
		await expect(page.locator('.templates-modal')).not.toBeVisible({ timeout: 3_000 });

		// 3. Create a new page via the sidebar button (uses default template).
		await createNewPage(page);

		// 4. Verify the new page is inside the folder.
		// The folder should be expanded and contain the new page as a child.
		const folderChildren = page.locator('.children .node-row');
		await expect(folderChildren.first()).toBeVisible({ timeout: 5_000 });
	});

	test('page created from template dropdown respects default folder', async ({ page }) => {
		// 1. Create a folder.
		await createNewFolder(page);
		const folderRow = page.locator('.node-row.folder').first();
		await expect(folderRow).toBeVisible();

		// 2. Set default folder on the default template.
		await openTemplateManager(page);
		const defaultRow = page.locator('.template-row').first();
		await defaultRow.locator('.action-btn:has-text("Edit")').click();

		const folderSelect = page.locator('#template-default-folder');
		await folderSelect.selectOption({ index: 1 });
		await page.locator('.modal-footer .btn-primary:has-text("Save")').click();
		await expect(page.locator('.templates-list')).toBeVisible({ timeout: 3_000 });
		await page.locator('.modal-footer .btn-ghost:has-text("Close")').click();
		await expect(page.locator('.templates-modal')).not.toBeVisible({ timeout: 3_000 });

		// 3. Create page via the template dropdown (hover → click template name).
		const wrapper = page.locator('.new-page-btn-wrapper');
		await wrapper.hover();
		const dropdown = page.locator('.template-dropdown');
		await expect(dropdown).toBeVisible({ timeout: 3_000 });
		await dropdown.locator('.dropdown-item').first().click();
		await page.mouse.move(0, 0);

		// Should navigate to the new page.
		await page.waitForURL(/\/notes\//, { timeout: 5_000 });

		// 4. Verify the page ended up inside the folder.
		const folderChildren = page.locator('.children .node-row');
		await expect(folderChildren.first()).toBeVisible({ timeout: 5_000 });
	});

	test('new template can be created with a default folder', async ({ page }) => {
		// 1. Create a folder.
		await createNewFolder(page);
		await expect(page.locator('.node-row.folder').first()).toBeVisible();

		// 2. Open template manager → create new template with folder set.
		await openTemplateManager(page);
		await page.locator('.btn-primary:has-text("+ New template")').click();

		// Fill name.
		await page.locator('#template-name').fill('Folder Template');

		// Set default folder.
		const folderSelect = page.locator('#template-default-folder');
		await folderSelect.selectOption({ index: 1 });

		// Save.
		await page.locator('.modal-footer .btn-primary:has-text("Save")').click();
		await expect(page.locator('.templates-list')).toBeVisible({ timeout: 3_000 });

		// Verify the new template appears in the list.
		await expect(page.locator('.template-name:has-text("Folder Template")')).toBeVisible();

		// Close modal.
		await page.locator('.modal-footer .btn-ghost:has-text("Close")').click();
		await expect(page.locator('.templates-modal')).not.toBeVisible({ timeout: 3_000 });

		// 3. Use the new template from the dropdown.
		const wrapper = page.locator('.new-page-btn-wrapper');
		await wrapper.hover();
		const dropdown = page.locator('.template-dropdown');
		await expect(dropdown).toBeVisible({ timeout: 3_000 });
		await dropdown.locator('.dropdown-item-name:has-text("Folder Template")').click();
		await page.mouse.move(0, 0);

		await page.waitForURL(/\/notes\//, { timeout: 5_000 });

		// Page should be inside the folder.
		const folderChildren = page.locator('.children .node-row');
		await expect(folderChildren.first()).toBeVisible({ timeout: 5_000 });
	});

	test('⌘N respects default folder on the default template', async ({ page }) => {
		// 1. Create a folder.
		await createNewFolder(page);
		await expect(page.locator('.node-row.folder').first()).toBeVisible();

		// 2. Set default folder on the default template.
		await openTemplateManager(page);
		const defaultRow = page.locator('.template-row').first();
		await defaultRow.locator('.action-btn:has-text("Edit")').click();

		const folderSelect = page.locator('#template-default-folder');
		await folderSelect.selectOption({ index: 1 });
		await page.locator('.modal-footer .btn-primary:has-text("Save")').click();
		await expect(page.locator('.templates-list')).toBeVisible({ timeout: 3_000 });
		await page.locator('.modal-footer .btn-ghost:has-text("Close")').click();
		await expect(page.locator('.templates-modal')).not.toBeVisible({ timeout: 3_000 });

		// 3. Click on the sidebar area to ensure focus is not in an input.
		await page.locator('.sidebar-header').click();

		// 4. Press ⌘N to create a new page.
		const currentUrl = page.url();
		await page.keyboard.press('Meta+n');
		await page.waitForURL((url) => url.pathname !== new URL(currentUrl).pathname, {
			timeout: 5_000
		});

		// 5. Verify page is in the folder.
		const folderChildren = page.locator('.children .node-row');
		await expect(folderChildren.first()).toBeVisible({ timeout: 5_000 });
	});

	test('template with no folder still creates page at root', async ({ page }) => {
		// Default template has no folder set — just create a new page.
		const initialNodes = await page.locator('.page-tree-container > .tree-node').count();

		await createNewPage(page);

		// The new page should be at root level (not inside any folder).
		// Count root-level tree nodes — should be one more than before.
		const afterNodes = await page.locator('.page-tree-container > .tree-node').count();
		expect(afterNodes).toBe(initialNodes + 1);
	});

	test('context menu "New from template" in folder ignores template default folder', async ({
		page
	}) => {
		// 1. Create two folders.
		await createNewFolder(page);
		await createNewFolder(page);
		const folders = page.locator('.node-row.folder');
		await expect(folders).toHaveCount(2);

		// 2. Set the default template's folder to folder #2.
		await openTemplateManager(page);
		const defaultRow = page.locator('.template-row').first();
		await defaultRow.locator('.action-btn:has-text("Edit")').click();

		const folderSelect = page.locator('#template-default-folder');
		// Select the second folder (index 2: 0=Root, 1=folder1, 2=folder2).
		await folderSelect.selectOption({ index: 2 });
		await page.locator('.modal-footer .btn-primary:has-text("Save")').click();
		await expect(page.locator('.templates-list')).toBeVisible({ timeout: 3_000 });
		await page.locator('.modal-footer .btn-ghost:has-text("Close")').click();
		await expect(page.locator('.templates-modal')).not.toBeVisible({ timeout: 3_000 });

		// 3. Right-click folder #1 → New from template → Default.
		const folder1 = folders.first();
		await folder1.click({ button: 'right' });
		const contextMenu = page.locator('.context-menu');
		await expect(contextMenu).toBeVisible();

		const templateItem = contextMenu.locator(
			'.context-item.has-submenu:has-text("New from template")'
		);
		await templateItem.hover();
		const submenu = page.locator('.template-submenu');
		await expect(submenu).toBeVisible({ timeout: 3_000 });
		await submenu.locator('.context-item').first().click();

		await page.waitForURL(/\/notes\//, { timeout: 5_000 });

		// 4. The page should be inside folder #1 (not folder #2).
		// Folder #1's children should contain the new page.
		const folder1Children = page
			.locator('.tree-node')
			.filter({ has: page.locator('.node-row.folder') })
			.first()
			.locator('.children .node-row');
		await expect(folder1Children.first()).toBeVisible({ timeout: 5_000 });
	});
});
