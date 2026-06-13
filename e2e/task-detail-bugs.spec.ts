import {
	test,
	expect,
	typeInEditor,
	navigateToTaskBoard,
	openSearchModal
} from './fixtures';

test.describe('Task detail page bugs', () => {
	test.describe.configure({ mode: 'serial' });

	/**
	 * Helper: creates a task via the TODO-bullet editor flow and navigates to its detail page.
	 */
	async function createTaskAndOpenDetail(page: import('@playwright/test').Page, title: string) {
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially(`- ${title}`, { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();
		await expect(popover).not.toBeVisible();

		await navigateToTaskBoard(page);
		await page.locator(`.card-title:has-text("${title}")`).click({ timeout: 5_000 });
		await expect(page.locator('.task-detail-page')).toBeVisible({ timeout: 5_000 });
	}

	test('date picker dropdown is visible and not clipped by meta-grid', async ({ page }) => {
		await createTaskAndOpenDetail(page, 'Date picker test');

		// Click the date picker trigger
		const datepickerTrigger = page.locator('.datepicker-trigger');
		await datepickerTrigger.click();

		// The dropdown should be visible
		const dropdown = page.locator('.datepicker-dropdown');
		await expect(dropdown).toBeVisible({ timeout: 3_000 });

		// Verify the dropdown is not clipped by overflow:hidden on .meta-grid.
		// Check that the last row of days is actually visible by evaluating
		// whether the element is within the visible (non-clipped) area.
		const isClipped = await page.evaluate(() => {
			const dropdown = document.querySelector('.datepicker-dropdown') as HTMLElement;
			if (!dropdown) return true;
			const rect = dropdown.getBoundingClientRect();
			// Walk up ancestors to check if any overflow:hidden clips this element
			let parent = dropdown.parentElement;
			while (parent) {
				const style = getComputedStyle(parent);
				if (style.overflow === 'hidden' || style.overflowY === 'hidden') {
					const parentRect = parent.getBoundingClientRect();
					// If dropdown extends below the parent's visible area, it's clipped
					if (rect.bottom > parentRect.bottom + 1) {
						return true;
					}
				}
				parent = parent.parentElement;
			}
			return false;
		});

		expect(isClipped).toBe(false);

		// Also verify the "Today" button at the bottom of the dropdown is clickable
		const todayBtn = dropdown.locator('.dp-footer-btn:has-text("Today")');
		await expect(todayBtn).toBeVisible();
		await todayBtn.click();

		// Dropdown should close after selection
		await expect(dropdown).not.toBeVisible({ timeout: 3_000 });
	});

	test('tags persist after adding and navigating away', async ({ page }) => {
		await createTaskAndOpenDetail(page, 'Tags test task');

		// Add a tag by typing in the tag input
		const tagInput = page.locator('.tag-text-input');
		await tagInput.click();
		await tagInput.fill('important');
		await tagInput.press('Enter');

		// The tag pill should appear
		await expect(page.locator('.tag-pill:has-text("important")')).toBeVisible();

		// Add another tag
		await tagInput.fill('urgent');
		await tagInput.press('Enter');
		await expect(page.locator('.tag-pill:has-text("urgent")')).toBeVisible();

		// Wait for save
		await page.waitForTimeout(1_000);

		// Navigate away and back
		await page.locator('.back-btn').click();
		await page.locator(`.card-title:has-text("Tags test task")`).click({ timeout: 5_000 });
		await expect(page.locator('.task-detail-page')).toBeVisible({ timeout: 5_000 });

		// Tags should still be there
		await expect(page.locator('.tag-pill:has-text("important")')).toBeVisible({ timeout: 3_000 });
		await expect(page.locator('.tag-pill:has-text("urgent")')).toBeVisible({ timeout: 3_000 });
	});

	test('URL validation rejects invalid URLs', async ({ page, storageMode }) => {
		// Link unfurl UI only shows in API mode
		test.skip(storageMode !== 'api', 'Link unfurl only available in API mode');

		await createTaskAndOpenDetail(page, 'URL validation task');

		const linkInput = page.locator('.link-input');
		await expect(linkInput).toBeVisible();

		// Test with a clearly invalid URL (no dots, not a real domain)
		await linkInput.fill('not a valid url');
		await linkInput.press('Enter');

		// Should show an error
		await expect(page.locator('.link-error')).toBeVisible({ timeout: 3_000 });

		// Clear and test with another invalid string
		await linkInput.fill('');
		await linkInput.fill('just-a-word');
		await linkInput.press('Enter');
		await expect(page.locator('.link-error')).toBeVisible({ timeout: 3_000 });
	});

	test('search finds tasks by title via modal', async ({ page }) => {
		// Create a task with a unique title
		await createTaskAndOpenDetail(page, 'UniqueSearchableTask98765');

		// Navigate away from task detail
		await page.locator('.back-btn').click();
		await page.waitForTimeout(500);

		// Open search modal
		await openSearchModal(page);

		// Search for the task
		await page.locator('.search-input').fill('UniqueSearchableTask98765');

		// Should find the task in results
		const taskResults = page.locator('.result-group:has(.group-label:has-text("Tasks")) .result-item');
		await expect(taskResults.first()).toBeVisible({ timeout: 5_000 });
		await expect(taskResults.first().locator('.result-title')).toContainText('UniqueSearchableTask98765');
	});

	test('search finds tasks on the full search page', async ({ page }) => {
		// Create a task with a unique title
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- SearchPageTask54321', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();
		await expect(popover).not.toBeVisible();

		// Wait for task to be saved
		await page.waitForTimeout(500);

		// Navigate to the full search page
		await page.locator('a.nav-item:has-text("Search")').click();
		await page.waitForSelector('.search-input', { timeout: 5_000 });

		// Search for the task
		await page.locator('.search-input').fill('SearchPageTask54321');

		// Should find the task
		const results = page.locator('.result-item');
		await expect(results.first()).toBeVisible({ timeout: 5_000 });
		await expect(results.first()).toContainText('SearchPageTask54321');
	});

	test('search results update when query is refined', async ({ page }) => {
		// Navigate to the search page
		await page.locator('a.nav-item:has-text("Search")').click();
		await page.waitForSelector('.search-input', { timeout: 5_000 });

		// Search for "Getting Started" (the default page)
		const searchInput = page.locator('.search-input');
		await searchInput.fill('Getting');

		// Should find results
		const results = page.locator('.result-item');
		await expect(results.first()).toBeVisible({ timeout: 5_000 });

		// Now refine to a non-existent query
		await searchInput.fill('zzznonexistentzzzz');

		// Results should disappear — either show "No results" or empty
		await expect(page.locator('.result-item')).not.toBeVisible({ timeout: 3_000 });
	});

	test('search modal results update after data changes', async ({ page }) => {
		// Open search modal and search for a term that won't match anything yet
		await openSearchModal(page);
		await page.locator('.search-panel .search-input').fill('LiveSearchTask999');

		// No results initially
		await expect(page.locator('.no-results')).toBeVisible({ timeout: 3_000 });

		// Close modal
		await page.keyboard.press('Escape');
		await expect(page.locator('.search-panel')).not.toBeVisible();

		// Create a task with that title
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# TODO', { delay: 30 });
		await editor.press('Enter');
		await editor.pressSequentially('- LiveSearchTask999', { delay: 30 });

		const popover = page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible({ timeout: 5_000 });
		await popover.locator('button.btn-primary').click();
		await expect(popover).not.toBeVisible();

		await page.waitForTimeout(500);

		// Open search modal again and search — should now find the task
		await openSearchModal(page);
		await page.locator('.search-panel .search-input').fill('LiveSearchTask999');

		const taskResults = page.locator('.result-group:has(.group-label:has-text("Tasks")) .result-item');
		await expect(taskResults.first()).toBeVisible({ timeout: 5_000 });
	});
});
