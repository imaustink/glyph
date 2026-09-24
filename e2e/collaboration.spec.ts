/**
 * Realtime collaborative editing, end to end: two browsers (Alice and Bob)
 * editing the same note through the collab service.
 *
 * The emphasis is data integrity: nothing either person types is lost, one
 * person's edit causes its side effects (tasks) exactly once, undo only
 * affects your own changes, viewers can't write, and a version restore can't
 * be undone by a client still holding the old document.
 */
import type { Browser, BrowserContext, Page, APIRequestContext } from '@playwright/test';
import { test, expect } from './fixtures';

test.skip(({ storageMode }) => storageMode !== 'api', 'collaboration needs the API backend');
test.skip(process.env.TEST_COLLAB_ENABLED === 'false', 'collaboration disabled for this run');

const JSON_HEADERS = { 'X-Requested-With': 'XMLHttpRequest', 'Content-Type': 'application/json' };
const SLOW = { timeout: 15_000 };

// ─── Page objects ─────────────────────────────────────────────────────────────

class Collaborator {
	constructor(
		readonly name: string,
		readonly context: BrowserContext,
		readonly page: Page
	) {}

	get api(): APIRequestContext {
		return this.context.request;
	}

	get editor() {
		return this.page.locator('main .tiptap-editor');
	}

	get status() {
		return this.page.locator('[data-testid="collab-status"]');
	}

	async open(pageId: string) {
		await this.page.goto(`/notes/${pageId}`);
		await expect(this.page.locator('.editor-wrapper')).toHaveAttribute('data-content-loaded', 'true', SLOW);
		await expect(this.status).toBeVisible(SLOW);
		await expect(this.status).toHaveAttribute('data-connection', 'connected', SLOW);
	}

	async type(text: string) {
		await this.editor.click();
		await this.page.keyboard.press(`${await this.mod()}+End`);
		await this.editor.pressSequentially(text, { delay: 25 });
	}

	/**
	 * Undo through the editor's own shortcut. TipTap binds "Mod" from
	 * navigator.platform, which headless Chromium on macOS doesn't report as
	 * a Mac, so Playwright's host-based ControlOrMeta would press the wrong
	 * key there (and fall through to the browser's native undo).
	 */
	async undo() {
		await this.page.keyboard.press(`${await this.mod()}+z`);
	}

	private async mod(): Promise<'Meta' | 'Control'> {
		return (await this.page.evaluate(() => /Mac|iPhone|iPad|iPod/.test(navigator.platform))) ? 'Meta' : 'Control';
	}

	/** Wait until the server has acknowledged every local change. */
	async synced() {
		await expect(this.status).toHaveAttribute('data-unsynced', 'false', SLOW);
	}

	async createPage(title: string): Promise<string> {
		// The TODO trigger is what the app's own "new page" sets; without it an
		// API-created page has an empty trigger and never detects TODO bullets.
		const res = await this.api.post('/api/v1/pages', {
			data: { title, type: 'page', todoTrigger: { pattern: 'TODO', matchMode: 'exact', blockTypes: ['heading'] } },
			headers: JSON_HEADERS
		});
		expect(res.status(), await res.text()).toBe(201);
		return ((await res.json()) as { id: string }).id;
	}

	async share(pageId: string, withUserId: string, permission: 'editor' | 'viewer') {
		const res = await this.api.post('/api/v1/shares', {
			data: { resourceType: 'page', resourceId: pageId, sharedWithId: withUserId, permission },
			headers: JSON_HEADERS
		});
		expect(res.status(), await res.text()).toBe(201);
	}

	async storedContent(pageId: string): Promise<{ text: string; revision: number }> {
		const res = await this.api.get(`/api/v1/pages/${pageId}/content`);
		if (res.status() === 404) return { text: '', revision: 0 };
		const body = (await res.json()) as { content: unknown; revision: number };
		return { text: JSON.stringify(body.content), revision: body.revision };
	}

	/** Close the task-details popover that opens for a new TODO bullet. */
	async closeTaskPopover() {
		const popover = this.page.locator('[role="dialog"][aria-label="Create task"]');
		await expect(popover).toBeVisible(SLOW);
		await popover.locator('button.btn-primary').click();
		await expect(popover).toBeHidden(SLOW);
	}

	async tasksFor(pageId: string): Promise<{ id: string; sourceNodeId: string | null; title: string }[]> {
		const res = await this.api.get(`/api/v1/tasks?sourcePageId=${pageId}`);
		return (await res.json()) as { id: string; sourceNodeId: string | null; title: string }[];
	}
}

async function collaborator(browser: Browser, baseURL: string, name: string, userId: string): Promise<Collaborator> {
	const context = await browser.newContext({ baseURL });
	const res = await context.request.post(`/test/become/${userId}`);
	expect(res.ok()).toBe(true);
	return new Collaborator(name, context, await context.newPage());
}

// ─── Specs ────────────────────────────────────────────────────────────────────

test.describe('Realtime collaboration', () => {
	let alice: Collaborator;
	let bob: Collaborator;
	let pageId: string;

	test.beforeEach(async ({ browser, baseURL, seedUsers }) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');
		alice = await collaborator(browser, baseURL, 'Alice', seedUsers.userA.id);
		bob = await collaborator(browser, baseURL, 'Bob', seedUsers.userB.id);
		pageId = await alice.createPage('Shared plan');
	});

	test.afterEach(async () => {
		await alice?.context.close();
		await bob?.context.close();
	});

	test('both people see each other type, and nothing is lost', async ({ seedUsers }) => {
		await alice.share(pageId, seedUsers!.userB.id, 'editor');
		await alice.open(pageId);
		await bob.open(pageId);

		await Promise.all([alice.type('Alice was here. '), bob.type('Bob too. ')]);

		for (const who of [alice, bob]) {
			await expect(who.editor).toContainText('Alice was here.', SLOW);
			await expect(who.editor).toContainText('Bob too.', SLOW);
		}
		await Promise.all([alice.synced(), bob.synced()]);

		// It's persisted: a fresh load has both…
		await bob.page.reload();
		await expect(bob.editor).toContainText('Alice was here.', SLOW);
		await expect(bob.editor).toContainText('Bob too.', SLOW);
		// …and so does the stored snapshot everything else reads.
		await expect.poll(async () => (await alice.storedContent(pageId)).text, SLOW).toContain('Alice was here.');
		await expect.poll(async () => (await alice.storedContent(pageId)).text, SLOW).toContain('Bob too.');
	});

	test('a TODO bullet creates exactly one task, however many people have the note open', async ({ seedUsers }) => {
		await alice.share(pageId, seedUsers!.userB.id, 'editor');
		await alice.open(pageId);
		await bob.open(pageId);

		await alice.type('# TODO');
		await alice.page.keyboard.press('Enter');
		await alice.editor.pressSequentially('- Ship collaboration', { delay: 25 });

		// Alice (the author) links it to a task; Bob's client must not create another.
		await expect(alice.page.locator('main .tiptap-editor li[data-task-id]')).toHaveCount(1, SLOW);
		await expect(bob.page.locator('main .tiptap-editor li[data-task-id]')).toHaveCount(1, SLOW);
		await alice.closeTaskPopover();
		await Promise.all([alice.synced(), bob.synced()]);
		await bob.page.waitForTimeout(1500);

		const tasks = await alice.tasksFor(pageId);
		expect(tasks).toHaveLength(1);
		expect(tasks[0].title).toContain('Ship collaboration');
	});

	test('deleting a bullet hides its task, and undo brings the same task back', async ({ seedUsers }) => {
		await alice.share(pageId, seedUsers!.userB.id, 'editor');
		await alice.open(pageId);
		await bob.open(pageId);

		await alice.type('# TODO');
		await alice.page.keyboard.press('Enter');
		await alice.editor.pressSequentially('- Keep me', { delay: 25 });
		await expect(alice.page.locator('main .tiptap-editor li[data-task-id]')).toHaveCount(1, SLOW);
		await alice.closeTaskPopover();
		await alice.synced();
		await expect.poll(async () => (await alice.tasksFor(pageId)).length, SLOW).toBe(1);
		const [task] = await alice.tasksFor(pageId);

		// Bob deletes the bullet: its text, then Backspace once more lifts the
		// (now empty) item out of the list.
		const bullet = bob.page.locator('main .tiptap-editor li[data-task-id] p');
		await bullet.click();
		await bob.page.keyboard.press('End');
		for (let i = 0; i < 'Keep me'.length + 2; i++) await bob.page.keyboard.press('Backspace', { delay: 20 });
		await expect(alice.page.locator('main .tiptap-editor li[data-task-id]')).toHaveCount(0, SLOW);
		await expect.poll(async () => (await alice.api.get(`/api/v1/tasks/${task.id}`)).status(), SLOW).toBe(404);

		// Bob undoes: the bullet — and the very same task — come back.
		await expect(async () => {
			await bob.undo();
			await expect(alice.page.locator(`main .tiptap-editor li[data-task-id="${task.id}"]`)).toHaveCount(1, { timeout: 1000 });
		}).toPass(SLOW);
		await expect(alice.editor).toContainText('Keep me', SLOW);
		await expect.poll(async () => (await alice.api.get(`/api/v1/tasks/${task.id}`)).status(), SLOW).toBe(200);
	});

	test('undo only reverts your own changes', async ({ seedUsers }) => {
		await alice.share(pageId, seedUsers!.userB.id, 'editor');
		await alice.open(pageId);
		await bob.open(pageId);

		await bob.type('Bob keeps this. ');
		await expect(alice.editor).toContainText('Bob keeps this.', SLOW);
		await alice.type('Alice regrets this.');
		await expect(bob.editor).toContainText('Alice regrets this.', SLOW);

		await alice.editor.click();
		for (let i = 0; i < 5; i++) await alice.undo();

		for (const who of [alice, bob]) {
			await expect(who.editor).not.toContainText('Alice regrets this.', SLOW);
			await expect(who.editor).toContainText('Bob keeps this.');
		}
	});

	test('a viewer can watch but not edit', async ({ seedUsers }) => {
		await alice.share(pageId, seedUsers!.userB.id, 'viewer');
		await alice.open(pageId);
		await bob.open(pageId);

		await expect(bob.status).toHaveAttribute('data-readonly', 'true', SLOW);
		await expect(bob.editor).toHaveAttribute('contenteditable', 'false');

		await alice.type('Only Alice writes.');
		await expect(bob.editor).toContainText('Only Alice writes.', SLOW);
	});

	test('restoring a version reloads everyone, and the old document cannot come back', async ({ seedUsers }) => {
		await alice.share(pageId, seedUsers!.userB.id, 'editor');
		await alice.open(pageId);
		await bob.open(pageId);

		await alice.type('Good version.');
		await alice.synced();
		await expect.poll(async () => (await alice.storedContent(pageId)).text, SLOW).toContain('Good version.');
		await bob.type(' Mistake!');
		await bob.synced();
		await expect.poll(async () => (await alice.storedContent(pageId)).text, SLOW).toContain('Mistake!');

		// Restore the newest version without the mistake.
		const versions = (await (await alice.api.get(`/api/v1/pages/${pageId}/content/versions`)).json()) as {
			id: number;
			content: unknown;
		}[];
		const good = versions.find((v) => {
			const t = JSON.stringify(v.content);
			return t.includes('Good version.') && !t.includes('Mistake');
		});
		expect(good, 'a version without the mistake').toBeTruthy();
		const res = await alice.api.post(`/api/v1/pages/${pageId}/content/versions/${good!.id}/restore`, { headers: JSON_HEADERS });
		expect(res.status(), await res.text()).toBe(200);

		// Both editors reload into the restored document. Bob's copy, which
		// still contained the mistake, must not have been merged back in.
		for (const who of [alice, bob]) {
			await expect(who.editor).toContainText('Good version.', SLOW);
			await expect(who.editor).not.toContainText('Mistake!', SLOW);
		}
		await bob.type(' After restore.');
		await bob.synced();
		await expect.poll(async () => (await alice.storedContent(pageId)).text, SLOW).toContain('After restore.');
		expect((await alice.storedContent(pageId)).text).not.toContain('Mistake!');
	});

	test('a whole-document REST write cannot overwrite a note being edited collaboratively', async () => {
		await alice.open(pageId);
		await alice.type('Live content.');
		await alice.synced();
		await expect.poll(async () => (await alice.storedContent(pageId)).text, SLOW).toContain('Live content.');

		const { revision } = await alice.storedContent(pageId);
		const res = await alice.api.put(`/api/v1/pages/${pageId}/content`, {
			data: { expectedRevision: revision, content: { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'CLOBBER' }] }] } },
			headers: JSON_HEADERS
		});
		expect(res.status()).toBe(409);
		expect(((await res.json()) as { code: string }).code).toBe('collaborative');
		await expect(alice.editor).not.toContainText('CLOBBER');
	});
});
