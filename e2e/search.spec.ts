import { test, expect, openSearchModal, typeInEditor, createNewPage } from './fixtures';

test.describe('Search', () => {
	test('open search modal with Cmd+K', async ({ page }) => {
		await openSearchModal(page);
		await expect(page.locator('.search-panel')).toBeVisible();
		await expect(page.locator('.search-input')).toBeFocused();
	});

	test('close search modal with Escape', async ({ page }) => {
		await openSearchModal(page);
		await expect(page.locator('.search-panel')).toBeVisible();

		await page.keyboard.press('Escape');
		await expect(page.locator('.search-panel')).not.toBeVisible();
	});

	test('search finds the Getting Started page', async ({ page }) => {
		await openSearchModal(page);
		await page.locator('.search-input').fill('Getting Started');

		// Wait for results to appear.
		const results = page.locator('.result-item');
		await expect(results.first()).toBeVisible({ timeout: 15_000 });
		await expect(results.first().locator('.result-title')).toContainText('Getting Started');
	});

	test('click search result navigates to page', async ({ page }) => {
		await openSearchModal(page);
		await page.locator('.search-input').fill('Getting Started');

		const result = page.locator('.result-item').first();
		await expect(result).toBeVisible({ timeout: 15_000 });
		await result.click();

		// Modal should close.
		await expect(page.locator('.search-panel')).not.toBeVisible();
		// Should be on the Getting Started page.
		await expect(page.locator('.page-title')).toHaveText('Getting Started');
	});

	test('search with no results shows empty state', async ({ page }) => {
		await openSearchModal(page);
		await page.locator('.search-input').fill('xyznonexistentzyx');

		await expect(page.locator('.no-results')).toBeVisible({ timeout: 15_000 });
	});

	test('search finds a renamed page', async ({ page }) => {
		// Rename the Getting Started page to something unique.
		await page.locator('.page-title').click();
		const input = page.locator('input.title-edit');
		await input.fill('UniqueSearchablePage12345');
		await input.press('Enter');

		// Wait for the store to update.
		await page.waitForTimeout(500);

		// Navigate to the full search page (which rebuilds the index on mount).
		await page.locator('a.nav-item:has-text("Search")').click();
		await page.waitForSelector('.search-input', { timeout: 15_000 });
		await page.locator('.search-input').fill('UniqueSearchablePage');

		const results = page.locator('.result-item');
		await expect(results.first()).toBeVisible({ timeout: 15_000 });
		await expect(results.first().locator('.result-title')).toContainText(
			'UniqueSearchablePage12345'
		);
	});

	test('full search page works', async ({ page }) => {
		// Navigate to the search page.
		await page.locator('a.nav-item:has-text("Search")').click();
		await page.waitForSelector('.search-input', { timeout: 15_000 });

		// Type a query.
		await page.locator('.search-input').fill('Getting Started');

		// Results should appear.
		const results = page.locator('.result-item');
		await expect(results.first()).toBeVisible({ timeout: 3_000 });
	});

	// Regression guard for #54 ("Search is super jittery"). The modal used to be
	// sized by its content and centered in the viewport, so every keypress that
	// changed the result count resized the panel and re-centered it vertically.
	// The fix gives the results list a fixed height, so the panel now occupies
	// the exact same box regardless of how many results are showing. This test
	// fails if that fixed height is ever reverted to a content-driven size
	// (e.g. `flex: 1`), which would bring the jitter back.
	test('search modal keeps a stable size and position across result counts (#54)', async ({
		page
	}) => {
		// Seed several pages that share a token so one query returns many results.
		for (let i = 1; i <= 4; i++) {
			await createNewPage(page);
			const titleInput = page.locator('input.title-edit');
			await titleInput.waitFor({ timeout: 15_000 });
			await titleInput.fill(`Nebula Cluster ${i}`);
			await titleInput.press('Enter');
			await expect(page.locator('.page-title')).toHaveText(`Nebula Cluster ${i}`, {
				timeout: 15_000
			});
		}

		const panel = page.locator('.search-panel');
		const results = page.locator('.result-item');

		// Type a query into the open modal, wait for its result state to settle,
		// and return the panel's bounding box.
		async function boxForQuery(query: string, state: 'results' | 'none') {
			await page.locator('.search-input').fill(query);
			if (state === 'none') {
				await expect(page.locator('.no-results')).toBeVisible({ timeout: 15_000 });
			} else {
				await expect(results.first()).toBeVisible({ timeout: 15_000 });
			}
			// Let the debounced search (120ms) and the one-time fade-in settle
			// before measuring.
			await page.waitForTimeout(300);
			const box = await panel.boundingBox();
			expect(box, 'search panel should have a bounding box').not.toBeNull();
			return box!;
		}

		await openSearchModal(page);

		const many = await boxForQuery('Nebula', 'results');
		const manyCount = await results.count();
		expect(manyCount).toBeGreaterThan(1);

		const one = await boxForQuery('Getting Started', 'results');
		const oneCount = await results.count();
		expect(oneCount).toBeGreaterThanOrEqual(1);
		// Sanity-check that the states genuinely differ in result count — that is
		// exactly what used to move the panel.
		expect(oneCount).toBeLessThan(manyCount);

		const none = await boxForQuery('zznomatchqz', 'none');

		// The panel's top (y) and height must be identical across all three
		// states: this is the anti-jitter invariant.
		expect(Math.abs(many.y - one.y)).toBeLessThanOrEqual(1);
		expect(Math.abs(many.y - none.y)).toBeLessThanOrEqual(1);
		expect(Math.abs(many.height - one.height)).toBeLessThanOrEqual(1);
		expect(Math.abs(many.height - none.height)).toBeLessThanOrEqual(1);
	});
});
