/**
 * E2E tests for OAuth client admin management (api mode only).
 *
 * Covers:
 * - Org owner: create client, one-time secret reveal, scope grid (editor
 *   implies viewer), redirect URIs, persistence across reload, multi-org
 *   scoping, secret rotation, revocation
 * - Non-owner gating: no entry point, and direct navigation shows an
 *   access-denied state rather than the admin UI
 * - Token audit: a token obtained via client_credentials shows up in the
 *   admin UI and revoking it from the UI actually invalidates it
 */

import { test, expect, switchUser } from './fixtures';

/** Create an org as the current user and return its id. */
async function createOrg(page: import('@playwright/test').Page, name: string): Promise<string> {
	await page.goto('/settings/orgs');
	await page.locator('input[placeholder="New organization name…"]').fill(name);
	await page.keyboard.press('Enter');
	await expect(page.locator('.org-name', { hasText: name })).toBeVisible();
	await page.locator('.org-name-btn', { hasText: name }).click();
	await page.waitForSelector('.oauth-link');
	const href = await page.locator('.oauth-link').getAttribute('href');
	if (!href) throw new Error('oauth-link href missing');
	return href.split('/')[3];
}

async function gotoOAuthClients(page: import('@playwright/test').Page, orgId: string) {
	await page.goto(`/settings/orgs/${orgId}/oauth-clients`);
	await page.waitForSelector('.oauth-clients-page');
}

test.describe('OAuth clients admin (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('org owner creates, configures, rotates, and revokes a client', async ({ page }) => {
		const orgId = await createOrg(page, 'OAuth Test Org');
		await gotoOAuthClients(page, orgId);

		// ── Create ──────────────────────────────────────────────────────────
		await page.locator('.create-form input').fill('Agent One');
		await page.locator('.create-form .btn-primary').click();

		await expect(page.locator('.secret-reveal')).toBeVisible();
		await expect(page.locator('.secret-warning')).toContainText("won't be able to see it again");
		const secret1 = await page.locator('.secret-value').innerText();
		expect(secret1.length).toBeGreaterThan(10);

		await page.locator('.secret-reveal .dismiss').click();
		await expect(page.locator('.secret-reveal')).toHaveCount(0);
		await expect(page.locator('.client-item')).toHaveCount(1);
		await expect(page.locator('.status-badge.active')).toBeVisible();

		// ── Scopes: Editor implies/disables Viewer ─────────────────────────
		const pageRow = page.locator('.scope-grid tbody tr').nth(0);
		await pageRow.locator('td').nth(2).locator('input[type=checkbox]').check();
		await expect(pageRow.locator('td').nth(1).locator('input[type=checkbox]')).toBeChecked();
		await expect(pageRow.locator('td').nth(1).locator('input[type=checkbox]')).toBeDisabled();

		// ── Redirect URI ────────────────────────────────────────────────────
		await page.locator('.add-redirect-row input').fill('https://agent.example.com/callback');
		await page.locator('.add-redirect-row .btn-ghost').click();
		await expect(page.locator('.redirect-row code')).toHaveText(
			'https://agent.example.com/callback'
		);

		// ── Save & verify persistence across reload ────────────────────────
		await page.locator('.actions-row .btn-primary').click();
		await expect(page.locator('.field-error')).toHaveCount(0);

		await page.reload();
		await page.waitForSelector('.oauth-clients-page');
		await page.locator('.client-name-btn').click();

		await expect(page.locator('.redirect-row code')).toHaveText(
			'https://agent.example.com/callback'
		);
		const pageRowAfterReload = page.locator('.scope-grid tbody tr').nth(0);
		await expect(pageRowAfterReload.locator('td').nth(2).locator('input[type=checkbox]')).toBeChecked();
		await expect(pageRowAfterReload.locator('td').nth(1).locator('input[type=checkbox]')).toBeChecked();

		// ── Multi-org scoping: add and remove a second owned org ───────────
		await page.goto('/settings/orgs');
		await page.locator('input[placeholder="New organization name…"]').fill('Second Org');
		await page.keyboard.press('Enter');
		await expect(page.locator('.org-name', { hasText: 'Second Org' })).toBeVisible();

		await gotoOAuthClients(page, orgId);
		await page.locator('.client-name-btn').click();
		await page.locator('.add-org-row select').selectOption({ label: 'Second Org' });
		await page.locator('.add-org-row .btn-ghost').click();
		await expect(page.locator('.org-chip')).toHaveCount(2);

		await page.locator('.org-chip', { hasText: 'Second Org' }).locator('.chip-remove').click();
		await expect(page.locator('.org-chip')).toHaveCount(1);

		// ── Rotate secret ────────────────────────────────────────────────────
		page.once('dialog', (d) => d.accept());
		await page.locator('.actions-row .btn-ghost', { hasText: 'Rotate secret' }).click();
		await expect(page.locator('.secret-reveal')).toBeVisible();
		const secret2 = await page.locator('.secret-value').innerText();
		expect(secret2).not.toBe(secret1);
		await page.locator('.secret-reveal .dismiss').click();

		// ── Revoke ──────────────────────────────────────────────────────────
		page.once('dialog', (d) => d.accept());
		await page
			.locator('.actions-row .btn-ghost.danger', { hasText: 'Revoke client' })
			.click();
		await expect(page.locator('.status-badge.revoked').first()).toBeVisible();
		await expect(page.locator('.actions-row .btn-ghost.danger')).toHaveCount(0);
	});

	test('non-owner cannot see or reach OAuth client management', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		const orgId = await createOrg(page, 'Gate Test Org');

		// Add bob as a viewer (non-owner)
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(2);

		await switchUser(page, baseURL, seedUsers.userB.id);
		await page.goto('/settings/orgs');
		await page.locator('.org-name-btn').click();

		// No entry point for non-owners.
		await expect(page.locator('.oauth-link')).toHaveCount(0);

		// Direct navigation shows an access-denied state, not the admin UI.
		await page.goto(`/settings/orgs/${orgId}/oauth-clients`);
		await expect(
			page.locator('.empty-state', { hasText: 'Only organization owners can manage OAuth clients' })
		).toBeVisible();
		await expect(page.locator('.clients-list')).toHaveCount(0);
		await expect(page.locator('.client-detail-panel')).toHaveCount(0);
	});

	test('token audit: issued token appears in the UI and revoking it invalidates it', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		const orgId = await createOrg(page, 'Token Audit Org');

		// Make bob a member so he's a valid delegation subject for this org.
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(2);

		await gotoOAuthClients(page, orgId);
		await page.locator('.create-form input').fill('Agent Audit');
		await page.locator('.create-form .btn-primary').click();
		await expect(page.locator('.secret-reveal')).toBeVisible();

		const clientId = (await page.locator('.client-id-value').innerText()).trim();
		const secret = (await page.locator('.secret-value').innerText()).trim();
		await page.locator('.secret-reveal .dismiss').click();

		// Obtain a token as the agent, delegated to act as bob.
		const tokenRes = await page.request.post(`${baseURL}/oauth/token`, {
			form: {
				grant_type: 'client_credentials',
				client_id: clientId,
				client_secret: secret,
				subject: seedUsers.userB.email,
				org_id: orgId
			}
		});
		expect(tokenRes.ok()).toBeTruthy();
		const tokenBody = (await tokenRes.json()) as { access_token: string };
		expect(tokenBody.access_token).toBeTruthy();

		// Reload the admin panel and confirm the token shows up.
		await page.reload();
		await page.waitForSelector('.oauth-clients-page');
		await page.locator('.client-name-btn').click();
		await expect(page.locator('.token-row')).toHaveCount(1);
		await expect(page.locator('.token-user')).toContainText(seedUsers.userB.email);

		// Confirm the token currently authenticates.
		const check1 = await page.request.get(`${baseURL}/api/v1/orgs`, {
			headers: { Authorization: `Bearer ${tokenBody.access_token}` }
		});
		expect(check1.ok()).toBeTruthy();

		// Revoke it from the UI.
		await page.locator('.token-row .icon-btn.danger').click();
		await expect(page.locator('.token-row')).toHaveCount(0);
		await expect(page.locator('.empty-state', { hasText: 'No active tokens' })).toBeVisible();

		// Confirm it no longer authenticates.
		const check2 = await page.request.get(`${baseURL}/api/v1/orgs`, {
			headers: { Authorization: `Bearer ${tokenBody.access_token}` }
		});
		expect(check2.status()).toBe(401);
	});

	test('revoke-all-tokens invalidates every active token for a client', async ({
		page,
		seedUsers,
		baseURL
	}) => {
		if (!seedUsers || !baseURL) throw new Error('fixtures missing');

		const orgId = await createOrg(page, 'Revoke All Org');
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userA.email);
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(2);
		await page.locator('.add-member-row input[type="email"]').fill(seedUsers.userB.email);
		await page.locator('.add-member-row .btn-primary').click();
		await expect(page.locator('.member-row')).toHaveCount(3);

		await gotoOAuthClients(page, orgId);
		await page.locator('.create-form input').fill('Agent Multi');
		await page.locator('.create-form .btn-primary').click();
		await expect(page.locator('.secret-reveal')).toBeVisible();
		const clientId = (await page.locator('.client-id-value').innerText()).trim();
		const secret = (await page.locator('.secret-value').innerText()).trim();
		await page.locator('.secret-reveal .dismiss').click();

		const tokens: string[] = [];
		for (const subjectEmail of [seedUsers.userA.email, seedUsers.userB.email]) {
			const res = await page.request.post(`${baseURL}/oauth/token`, {
				form: {
					grant_type: 'client_credentials',
					client_id: clientId,
					client_secret: secret,
					subject: subjectEmail,
					org_id: orgId
				}
			});
			expect(res.ok()).toBeTruthy();
			const body = (await res.json()) as { access_token: string };
			tokens.push(body.access_token);
		}

		await page.reload();
		await page.waitForSelector('.oauth-clients-page');
		await page.locator('.client-name-btn').click();
		await expect(page.locator('.token-row')).toHaveCount(2);

		page.once('dialog', (d) => d.accept());
		await page.locator('.tokens-header .btn-ghost.danger', { hasText: 'Revoke all' }).click();
		await expect(page.locator('.token-row')).toHaveCount(0);

		for (const accessToken of tokens) {
			const check = await page.request.get(`${baseURL}/api/v1/orgs`, {
				headers: { Authorization: `Bearer ${accessToken}` }
			});
			expect(check.status()).toBe(401);
		}
	});
});
