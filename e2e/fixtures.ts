/**
 * Custom Playwright fixtures for Glyph E2E tests.
 *
 * Provides a `test` export that automatically resets storage before each test
 * based on the active project's `storageMode` ("local" | "api").
 *
 * To add a new backend:
 *   1. Add a branch in `resetStorage` that clears data for your backend.
 *   2. Register a project in playwright.config.ts with a matching storageMode.
 */

import { test as base, expect } from '@playwright/test';

// Re-export expect for convenience.
export { expect };

type StorageMode = 'local' | 'api';

/** Extra fixtures available in every test. */
interface GlyphFixtures {
	/** Which storage backend is being exercised. */
	storageMode: StorageMode;
	/** Seed users from /test/reset (api mode only). */
	seedUsers: { userA: { id: string; email: string; name: string }; userB: { id: string; email: string; name: string } } | null;
}

export const test = base.extend<GlyphFixtures>({
	storageMode: [
		async ({}, use, testInfo) => {
			const mode = (testInfo.project.use as Record<string, unknown>).storageMode as
				| StorageMode
				| undefined;
			await use(mode ?? 'local');
		},
		{ scope: 'test' }
	],

	// Seed users populated from /test/reset in api mode.
	seedUsers: [
		async ({ storageMode, baseURL }, use) => {
			if (storageMode !== 'api') {
				await use(null);
				return;
			}
			const res = await fetch(`${baseURL}/test/reset`, { method: 'POST' });
			if (!res.ok) throw new Error(`/test/reset failed: ${res.status}`);
			const data = (await res.json()) as {
				userA: { id: string; email: string; name: string };
				userB: { id: string; email: string; name: string };
			};
			await use({ userA: data.userA, userB: data.userB });
		},
		{ scope: 'test' }
	],

	// Automatically reset storage before each test.
	page: async ({ page, storageMode, baseURL, seedUsers }, use) => {
		// In api mode, always call /test/reset so every test starts with a
		// clean DB where the dev user (sub=dev-user) is the only user.
		// Tests that use seedUsers already reset via that fixture, so skip here
		// to avoid a double reset.
		if (storageMode === 'api' && !seedUsers && baseURL) {
			const res = await fetch(`${baseURL}/test/reset`, { method: 'POST' });
			if (!res.ok) throw new Error(`/test/reset (page fixture) failed: ${res.status}`);
		}

		// Navigate to the app root — layout will load stores and auto-create
		// the "Getting Started" page if none exist.
		await page.goto('/', { waitUntil: 'commit' });

		// Wait for the app shell to finish loading.
		await page.waitForSelector('.app-shell .sidebar', { timeout: 30_000 });

		// The layout's onMount creates a "Getting Started" page (if none exist)
		// and navigates away from /. Wait for that navigation to settle so tests
		// see a stable sidebar state.
		if (page.url().endsWith('/') || page.url().endsWith('/#')) {
			await page.waitForURL((url) => url.pathname !== '/', { timeout: 15_000 }).catch(() => {});
		}

		await use(page);
	}
});

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Wait for a SvelteKit client-side navigation to settle. */
export async function waitForNavigation(page: import('@playwright/test').Page) {
	await page.waitForLoadState('networkidle');
}

/** Get the visible page title (h1) on the notes page. */
export async function getPageTitle(page: import('@playwright/test').Page) {
	return page.locator('.page-title').innerText();
}

/** Click the "New page" button in the sidebar (default template). */
export async function createNewPage(page: import('@playwright/test').Page) {
	const currentUrl = page.url();
	await page.locator('.section-actions button[title="New page (default template)"]').click();
	// Move mouse away so the template dropdown closes (it's hover-triggered).
	await page.mouse.move(0, 0);
	// Wait for SvelteKit to navigate to the new page.
	await page.waitForURL((url) => url.pathname !== new URL(currentUrl).pathname, {
		timeout: 15_000
	});
	// New pages open in title-editing mode (input.title-edit) so .page-title
	// may not exist yet. Wait for either element.
	await page.locator('.page-title, input.title-edit').first().waitFor({ timeout: 15_000 });
}

/** Click the "New folder" button in the sidebar. */
export async function createNewFolder(page: import('@playwright/test').Page) {
	await page.locator('.section-actions button[title="New folder"]').click();
}

/** Type into the TipTap editor. */
export async function typeInEditor(page: import('@playwright/test').Page, text: string) {
	const editor = page.locator('main .tiptap-editor');
	await editor.click();
	await editor.pressSequentially(text, { delay: 30 });
}

/** Get the text content of the TipTap editor. */
export async function getEditorText(page: import('@playwright/test').Page) {
	return page.locator('main .tiptap-editor').innerText();
}

/** Navigate to a page by clicking its title in the sidebar. */
export async function navigateToPage(page: import('@playwright/test').Page, title: string) {
	await page.locator(`.node-label:has-text("${title}")`).click();
	// Wait for the editor to mount on the new page.
	await page.waitForSelector('main .tiptap-editor', { timeout: 15_000 });
}

/** Navigate to the task board. */
export async function navigateToTaskBoard(page: import('@playwright/test').Page) {
	await page.locator('a.nav-item:has-text("Task Board")').click();
	await page.waitForSelector('.board-page', { timeout: 15_000 });
}

/** Navigate to search page. */
export async function navigateToSearch(page: import('@playwright/test').Page) {
	await page.locator('a.nav-item:has-text("Search")').click();
	await page.waitForSelector('.search-input', { timeout: 15_000 });
}

/** Open the ⌘K search modal. */
export async function openSearchModal(page: import('@playwright/test').Page) {
	await page.keyboard.press('Meta+k');
	await page.waitForSelector('.search-panel', { timeout: 15_000 });
}
/**
 * Switch the active session to a different user via the dev-only endpoint.
 * Only usable in api mode. After switching, navigates to / and waits for sidebar.
 */
export async function switchUser(
        page: import('@playwright/test').Page,
        baseURL: string,
        userId: string
) {
        await page.request.post(`${baseURL}/test/become/${userId}`);
        await page.goto('/', { waitUntil: 'commit' });
        await page.waitForSelector('.app-shell .sidebar', { timeout: 30_000 });
}