import { test, expect, typeInEditor, getEditorText, createNewPage, navigateToPage } from './fixtures';

test.describe('Editor', () => {
	test('type plain text in the editor', async ({ page }) => {
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('Hello, Glyph!', { delay: 20 });

		const text = await getEditorText(page);
		expect(text).toContain('Hello, Glyph!');
	});

	test('create a heading with # shortcut', async ({ page }) => {
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('# My Heading', { delay: 20 });

		// The input rule converts `# ` into an h1. The default template may
		// already have a TODO h1, so match by text.
		const h1 = page.locator('main .tiptap-editor h1:has-text("My Heading")');
		await expect(h1).toBeVisible();
	});

	test('create a bullet list with - shortcut', async ({ page }) => {
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('- First item', { delay: 20 });
		await editor.press('Enter');
		await editor.pressSequentially('Second item', { delay: 20 });

		// Should have bullet list items (default template may add more).
		const listItems = page.locator('main .tiptap-editor li');
		const count = await listItems.count();
		expect(count).toBeGreaterThanOrEqual(2);
	});

	test('editor content persists after navigating away and back', async ({ page }) => {
		// Type content in the Getting Started page.
		const editor = page.locator('main .tiptap-editor');
		await editor.click();
		await editor.pressSequentially('Persistent content here', { delay: 20 });

		// Wait for the debounced save to fire.
		await page.waitForTimeout(1500);

		// Create a new page and navigate to it.
		await createNewPage(page);
		// New page may open in title-edit mode; press Escape to dismiss.
		const titleInput = page.locator('input.title-edit');
		if (await titleInput.isVisible()) {
			await titleInput.press('Escape');
		}

		// Navigate back to Getting Started.
		await navigateToPage(page, 'Getting Started');
		await page.waitForTimeout(500);

		// Content should still be there.
		const text = await getEditorText(page);
		expect(text).toContain('Persistent content here');
	});

	test('create multiple heading levels', async ({ page }) => {
		const editor = page.locator('main .tiptap-editor');
		await editor.click();

		await editor.pressSequentially('# Heading 1', { delay: 20 });
		await editor.press('Enter');
		await editor.pressSequentially('Some text', { delay: 20 });
		await editor.press('Enter');
		await editor.pressSequentially('## Heading 2', { delay: 20 });
		await editor.press('Enter');
		await editor.pressSequentially('### Heading 3', { delay: 20 });

		// Use text-specific selectors since the default template may include headings.
		await expect(page.locator('main .tiptap-editor h1:has-text("Heading 1")')).toBeVisible();
		await expect(page.locator('main .tiptap-editor h2:has-text("Heading 2")')).toBeVisible();
		await expect(page.locator('main .tiptap-editor h3:has-text("Heading 3")')).toBeVisible();
	});
});
