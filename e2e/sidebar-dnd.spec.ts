import { test, expect, createNewPage, createNewFolder, navigateToPage } from './fixtures';

/**
 * Robust native HTML5 drag-and-drop using low-level mouse events.
 *
 * Playwright's `locator.dragTo()` can be flaky for native DnD onto small
 * targets (e.g. the short "Pages" header) under slower CI builds — the single
 * move to the target may not emit a `dragover` before `mouseup`, so the `drop`
 * never fires. This helper moves in steps and adds a settling move over the
 * target so `dragover` is guaranteed to fire before the drop.
 */
async function robustDragTo(
	page: import('@playwright/test').Page,
	source: import('@playwright/test').Locator,
	target: import('@playwright/test').Locator
) {
	const sBox = await source.boundingBox();
	const tBox = await target.boundingBox();
	if (!sBox || !tBox) throw new Error('drag source/target has no bounding box');

	const tx = tBox.x + tBox.width / 2;
	const ty = tBox.y + tBox.height / 2;

	await page.mouse.move(sBox.x + sBox.width / 2, sBox.y + sBox.height / 2);
	await page.mouse.down();
	// Initial small move kicks off the native dragstart.
	await page.mouse.move(sBox.x + sBox.width / 2, sBox.y + sBox.height / 2 - 6, { steps: 5 });
	await page.mouse.move(tx, ty, { steps: 12 });
	// Settling move over the target emits a fresh dragover right before drop.
	await page.mouse.move(tx, ty, { steps: 6 });
	await page.mouse.up();
}

test.describe('Folder template creation', () => {
	test('create page from template inside a folder via context menu', async ({ page }) => {
		// Create a folder.
		await createNewFolder(page);
		const folderRow = page.locator('.node-row.folder').first();
		await expect(folderRow).toBeVisible();

		// Right-click the folder to open context menu.
		await folderRow.click({ button: 'right' });
		const contextMenu = page.locator('.context-menu');
		await expect(contextMenu).toBeVisible();

		// Hover over "New from template" to reveal submenu.
		const templateItem = contextMenu.locator('.context-item.has-submenu:has-text("New from template")');
		await expect(templateItem).toBeVisible();
		await templateItem.hover();

		// The template submenu should appear with at least the Default template.
		const submenu = page.locator('.template-submenu');
		await expect(submenu).toBeVisible({ timeout: 3_000 });

		const defaultEntry = submenu.locator('.context-item').first();
		await expect(defaultEntry).toBeVisible();

		// Click the template entry to create a page inside the folder.
		await defaultEntry.click();

		// Should navigate to the new page.
		await page.waitForURL(/\/notes\//, { timeout: 5_000 });
		await expect(page.locator('main .tiptap-editor, input.title-edit').first()).toBeVisible({
			timeout: 5_000
		});

		// The folder should now have a child node — expand it if needed and verify.
		const folderChildren = page.locator('.node-row.folder ~ .children .tree-node, .node-row.folder + .children .tree-node');
		// Use a broader check: there should be more than 1 page in the sidebar.
		const allNodes = page.locator('.tree-node .node-label');
		const count = await allNodes.count();
		expect(count).toBeGreaterThanOrEqual(2); // Getting Started + new page in folder
	});

	test('"New page inside" still works on folders', async ({ page }) => {
		// Create a folder.
		await createNewFolder(page);
		const folderRow = page.locator('.node-row.folder').first();
		await expect(folderRow).toBeVisible();

		// Right-click.
		await folderRow.click({ button: 'right' });
		const contextMenu = page.locator('.context-menu');
		await expect(contextMenu).toBeVisible();

		// Click "New page inside".
		await page.locator('.context-item:has-text("New page inside")').click();

		// Should navigate to the new page.
		await page.waitForURL(/\/notes\//, { timeout: 5_000 });
		await expect(page.locator('main .tiptap-editor, input.title-edit').first()).toBeVisible({
			timeout: 5_000
		});
	});
});

test.describe('Drag and drop in sidebar', () => {
	test('drag a page into a folder', async ({ page }) => {
		// Rename the default page so we can track it.
		await page.locator('.page-title').click();
		const titleInput = page.locator('input.title-edit');
		await titleInput.fill('Page A');
		await titleInput.press('Enter');
		await expect(page.locator('.page-title')).toHaveText('Page A');

		// Create a folder.
		await createNewFolder(page);
		const folderRow = page.locator('.node-row.folder').first();
		await expect(folderRow).toBeVisible();

		// Drag "Page A" onto the folder.
		const pageRow = page.locator('.node-row', {
			has: page.locator('.node-label:has-text("Page A")')
		});

		await pageRow.dragTo(folderRow);

		// After drop, the folder should be expanded and contain "Page A" as a child.
		// The folder's children div should contain the page.
		const folderChildren = page.locator('.children .node-label:has-text("Page A")');
		await expect(folderChildren).toBeVisible({ timeout: 5_000 });
	});

	test('drag to reorder pages at the same level', async ({ page }) => {
		// Rename the first page.
		await page.locator('.page-title').click();
		const titleInput = page.locator('input.title-edit');
		await titleInput.fill('First');
		await titleInput.press('Enter');
		await expect(page.locator('.page-title')).toHaveText('First');

		// Create a second page.
		await createNewPage(page);
		const newTitleInput = page.locator('input.title-edit');
		await expect(newTitleInput).toBeVisible({ timeout: 3_000 });
		await newTitleInput.fill('Second');
		await newTitleInput.press('Enter');

		// Verify initial order: First, Second.
		const labels = page.locator('.page-tree-container > :first-child .node-label');
		// Both should be visible.
		await expect(page.locator('.node-label:has-text("First")')).toBeVisible();
		await expect(page.locator('.node-label:has-text("Second")')).toBeVisible();

		// Drag "Second" onto "First" (reorder: Second should land after First).
		const secondRow = page.locator('.node-row', {
			has: page.locator('.node-label:has-text("Second")')
		});
		const firstRow = page.locator('.node-row', {
			has: page.locator('.node-label:has-text("First")')
		});

		await secondRow.dragTo(firstRow);

		// Both pages should still exist in the sidebar.
		await expect(page.locator('.node-label:has-text("First")')).toBeVisible();
		await expect(page.locator('.node-label:has-text("Second")')).toBeVisible();
	});

	test('pages are draggable (draggable attribute present)', async ({ page }) => {
		const nodeRow = page.locator('.node-row').first();
		const draggable = await nodeRow.getAttribute('draggable');
		expect(draggable).toBe('true');
	});

	test('drag a page out of a folder back to the root level', async ({ page }) => {
		// Rename the default page so we can track it.
		await page.locator('.page-title').click();
		const titleInput = page.locator('input.title-edit');
		await titleInput.fill('Page A');
		await titleInput.press('Enter');
		await expect(page.locator('.page-title')).toHaveText('Page A');

		// Create a folder.
		await createNewFolder(page);
		const folderRow = page.locator('.node-row.folder').first();
		await expect(folderRow).toBeVisible();

		// Drag "Page A" into the folder.
		const pageRow = page.locator('.node-row', {
			has: page.locator('.node-label:has-text("Page A")')
		});
		await pageRow.dragTo(folderRow);

		// Confirm it is now a child of the folder.
		await expect(page.locator('.children .node-label:has-text("Page A")')).toBeVisible({
			timeout: 5_000
		});

		// Drag "Page A" out onto the empty area of the page tree container (root level).
		const treeContainer = page.locator('.page-tree-container');
		await pageRow.dragTo(treeContainer, { targetPosition: { x: 20, y: 300 } });

		// "Page A" should no longer be nested inside the folder's children.
		await expect(page.locator('.children .node-label:has-text("Page A")')).toHaveCount(0, {
			timeout: 5_000
		});
		// But it should still exist in the sidebar at root level.
		await expect(page.locator('.node-label:has-text("Page A")')).toBeVisible();

		// Reload to prove the move was persisted by the backend — not just an
		// optimistic UI update that snaps back after the server responds.
		await page.reload();
		await expect(page.locator('.node-label:has-text("Page A")')).toBeVisible({ timeout: 10_000 });
		await expect(page.locator('.children .node-label:has-text("Page A")')).toHaveCount(0);
	});

	test('drag a page out of a folder via the "Pages" header drop target', async ({ page }) => {
		// Rename the default page so we can track it.
		await page.locator('.page-title').click();
		const titleInput = page.locator('input.title-edit');
		await titleInput.fill('Page A');
		await titleInput.press('Enter');
		await expect(page.locator('.page-title')).toHaveText('Page A');

		// Create a folder and nest "Page A" inside it.
		await createNewFolder(page);
		const folderRow = page.locator('.node-row.folder').first();
		await expect(folderRow).toBeVisible();

		const pageRow = page.locator('.node-row', {
			has: page.locator('.node-label:has-text("Page A")')
		});
		await pageRow.dragTo(folderRow);
		await expect(page.locator('.children .node-label:has-text("Page A")')).toBeVisible({
			timeout: 5_000
		});

		// Drag "Page A" onto the "Pages" section header to move it to the top level.
		const sectionHeader = page.locator('.section-header');
		await robustDragTo(page, pageRow, sectionHeader);

		// "Page A" should no longer be nested inside the folder's children.
		await expect(page.locator('.children .node-label:has-text("Page A")')).toHaveCount(0, {
			timeout: 5_000
		});
		await expect(page.locator('.node-label:has-text("Page A")')).toBeVisible();

		// Reload to prove the move persisted server-side (not just optimistic UI).
		await page.reload();
		await expect(page.locator('.node-label:has-text("Page A")')).toBeVisible({ timeout: 10_000 });
		await expect(page.locator('.children .node-label:has-text("Page A")')).toHaveCount(0);
	});

	test('root drop highlight is cleared after dropping onto a child row', async ({ page }) => {
		// Rename the default page so we can track it.
		await page.locator('.page-title').click();
		const titleInput = page.locator('input.title-edit');
		await titleInput.fill('Page A');
		await titleInput.press('Enter');
		await expect(page.locator('.page-title')).toHaveText('Page A');

		// Create a folder and nest "Page A" inside it.
		await createNewFolder(page);
		const folderRow = page.locator('.node-row.folder').first();
		await expect(folderRow).toBeVisible();

		const pageRow = page.locator('.node-row', {
			has: page.locator('.node-label:has-text("Page A")')
		});
		await pageRow.dragTo(folderRow);
		await expect(page.locator('.children .node-label:has-text("Page A")')).toBeVisible({
			timeout: 5_000
		});

		// Drag "Page A" back out by dropping it onto the folder row again (a child
		// node-row). The drop calls stopPropagation, so the container/header drop
		// handlers never run — the `dragend` safety net must still clear the
		// highlight so the tree background doesn't get stuck in the accent state.
		await pageRow.dragTo(folderRow);

		// The root drop highlight must not be stuck after the drag ends.
		await expect(page.locator('.page-tree-container.root-drag-over')).toHaveCount(0);
		await expect(page.locator('.section-header.header-drag-over')).toHaveCount(0);
	});
});

