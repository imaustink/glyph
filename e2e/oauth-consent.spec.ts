/**
 * E2E tests for the OAuth authorization-code consent screen (api mode only).
 *
 * Covers:
 * - Happy path: Allow redirects to redirect_uri with code+state
 * - Deny path: redirects with error=access_denied+state
 * - Error states: invalid client_id / mismatched redirect_uri show an inline
 *   error and never redirect anywhere (open-redirect safety)
 *
 * NOTE on unauthenticated round-trip: this repo's e2e "api" project runs the
 * Go server in dev-auth mode (no OIDC configured), where the auth middleware
 * always resolves to a user (falling back to a synthetic dev user when no
 * session cookie is present) rather than ever returning 401. That means the
 * "logged out -> redirected through login -> back to consent screen" flow
 * cannot be exercised end-to-end against this dev server; there's no way to
 * simulate "logged out" in this mode. The consent info endpoint's 401 branch
 * and the frontend's UnauthorizedError -> /auth/login?next= handling are
 * covered by handler-level Go tests instead. See consentAuthRedirect.spec
 * gap noted below (test.fixme).
 */

import crypto from 'node:crypto';
import { test, expect } from './fixtures';

function base64url(buf: Buffer): string {
	return buf.toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function makePkcePair(): { verifier: string; challenge: string } {
	const verifier = base64url(crypto.randomBytes(32));
	const challenge = base64url(crypto.createHash('sha256').update(verifier).digest());
	return { verifier, challenge };
}

const REDIRECT_URI = 'https://relying-party.example.test/callback';

/** Create an org (as the current user) and an OAuth client scoped to it with
 *  a redirect URI and page:read granted, returning identifiers needed to
 *  drive /oauth/authorize. */
async function createConsentTestClient(
	page: import('@playwright/test').Page,
	orgName: string,
	clientName: string
): Promise<{ orgId: string; clientId: string }> {
	await page.goto('/settings/orgs');
	await page.locator('input[placeholder="New organization name…"]').fill(orgName);
	await page.keyboard.press('Enter');
	await expect(page.locator('.org-name', { hasText: orgName })).toBeVisible();
	await page.locator('.org-name-btn', { hasText: orgName }).click();
	await page.waitForSelector('.oauth-link');
	const href = await page.locator('.oauth-link').getAttribute('href');
	if (!href) throw new Error('oauth-link href missing');
	const orgId = href.split('/')[3];

	await page.goto(`/settings/orgs/${orgId}/oauth-clients`);
	await page.waitForSelector('.oauth-clients-page');
	await page.locator('.create-form input').fill(clientName);
	await page.locator('.create-form .btn-primary').click();
	await expect(page.locator('.secret-reveal')).toBeVisible();
	const clientId = (await page.locator('.client-id-value').innerText()).trim();
	await page.locator('.secret-reveal .dismiss').click();

	// Grant page:read so the consent screen has a concrete scope to render.
	const pageRow = page.locator('.scope-grid tbody tr').nth(0);
	await pageRow.locator('td').nth(1).locator('input[type=checkbox]').check();

	// Register the redirect URI the consent flow will use.
	await page.locator('.add-redirect-row input').fill(REDIRECT_URI);
	await page.locator('.add-redirect-row .btn-ghost').click();
	await page.locator('.actions-row .btn-primary').click();
	await expect(page.locator('.field-error')).toHaveCount(0);

	return { orgId, clientId };
}

function authorizeUrl(params: {
	clientId: string;
	orgId: string;
	redirectUri: string;
	state: string;
	challenge: string;
}): string {
	const qs = new URLSearchParams({
		response_type: 'code',
		client_id: params.clientId,
		redirect_uri: params.redirectUri,
		scope: 'page:read',
		state: params.state,
		code_challenge: params.challenge,
		code_challenge_method: 'S256',
		org_id: params.orgId
	});
	return `/oauth/authorize?${qs.toString()}`;
}

test.describe('OAuth authorization-code consent screen (api)', () => {
	test.skip(({ storageMode }) => storageMode !== 'api', 'API mode only');

	test('renders client/org/scopes and Allow redirects with code+state', async ({ page }) => {
		const { orgId, clientId } = await createConsentTestClient(
			page,
			'Consent Allow Org',
			'Third Party App'
		);
		const { verifier, challenge } = makePkcePair();
		const state = 'state-allow-123';

		await page.route(`${REDIRECT_URI}**`, (route) =>
			route.fulfill({ status: 200, contentType: 'text/plain', body: 'ok' })
		);

		await page.goto(
			authorizeUrl({ clientId, orgId, redirectUri: REDIRECT_URI, state, challenge })
		);
		await page.waitForSelector('.consent-card');

		await expect(page.locator('.consent-card h1')).toContainText('Third Party App');
		await expect(page.locator('.consent-org')).toContainText('Consent Allow Org');
		await expect(page.locator('.scope-list li')).toContainText('View pages');

		await page.locator('.consent-actions .btn-primary', { hasText: 'Allow' }).click();
		await page.waitForURL(new RegExp(`^${REDIRECT_URI.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`));

		const url = new URL(page.url());
		expect(url.searchParams.get('state')).toBe(state);
		expect(url.searchParams.get('code')).toBeTruthy();
		expect(url.searchParams.get('error')).toBeNull();

		// PKCE verifier is available to whoever drove this flow; not exercised
		// further here since redeeming the code is covered by Go-level tests
		// (token_handler_test.go) and the client-credentials audit test above
		// already proves /oauth/token issues and enforces tokens end-to-end.
		expect(verifier.length).toBeGreaterThanOrEqual(43);
	});

	test('Deny redirects with error=access_denied and matching state', async ({ page }) => {
		const { orgId, clientId } = await createConsentTestClient(
			page,
			'Consent Deny Org',
			'Third Party App'
		);
		const { challenge } = makePkcePair();
		const state = 'state-deny-456';

		await page.route(`${REDIRECT_URI}**`, (route) =>
			route.fulfill({ status: 200, contentType: 'text/plain', body: 'ok' })
		);

		await page.goto(
			authorizeUrl({ clientId, orgId, redirectUri: REDIRECT_URI, state, challenge })
		);
		await page.waitForSelector('.consent-card');

		await page.locator('.consent-actions .btn-ghost', { hasText: 'Deny' }).click();
		await page.waitForURL(new RegExp(`^${REDIRECT_URI.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`));

		const url = new URL(page.url());
		expect(url.searchParams.get('state')).toBe(state);
		expect(url.searchParams.get('error')).toBe('access_denied');
		expect(url.searchParams.get('code')).toBeNull();
	});

	test('invalid client_id shows an inline error and never redirects', async ({ page }) => {
		const { challenge } = makePkcePair();
		let redirected = false;
		await page.route(`${REDIRECT_URI}**`, (route) => {
			redirected = true;
			return route.fulfill({ status: 200, body: 'ok' });
		});

		await page.goto(
			authorizeUrl({
				clientId: 'glyph_client_does_not_exist',
				orgId: '00000000-0000-0000-0000-000000000000',
				redirectUri: REDIRECT_URI,
				state: 'irrelevant',
				challenge
			})
		);

		await page.waitForSelector('.consent-card');
		await expect(page.locator('.consent-card h1')).toContainText("Can't complete this request");
		await expect(page.locator('.consent-error')).toBeVisible();
		expect(page.url()).toContain('/oauth/authorize');
		expect(redirected).toBe(false);
	});

	test('mismatched redirect_uri shows an inline error and never redirects', async ({ page }) => {
		const { orgId, clientId } = await createConsentTestClient(
			page,
			'Consent Mismatch Org',
			'Third Party App'
		);
		const { challenge } = makePkcePair();
		let redirected = false;
		await page.route('https://not-registered.example.test/**', (route) => {
			redirected = true;
			return route.fulfill({ status: 200, body: 'ok' });
		});

		await page.goto(
			authorizeUrl({
				clientId,
				orgId,
				redirectUri: 'https://not-registered.example.test/callback',
				state: 'irrelevant',
				challenge
			})
		);

		await page.waitForSelector('.consent-card');
		await expect(page.locator('.consent-card h1')).toContainText("Can't complete this request");
		await expect(page.locator('.consent-error')).toBeVisible();
		expect(page.url()).toContain('/oauth/authorize');
		expect(redirected).toBe(false);
	});

	// Dev-auth mode never returns 401 (see file header), so the real
	// logged-out -> /auth/login?next= -> back-to-consent-screen round trip
	// cannot be driven end-to-end here. Left as an explicit, documented gap
	// rather than a fake/misleading passing test.
	test.fixme(
		'unauthenticated user is redirected through login and back to the consent screen',
		async () => {}
	);
});
