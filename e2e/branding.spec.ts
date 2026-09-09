import { test, expect } from './fixtures';

/**
 * Brand asset coverage.
 *
 * The logo is hand-authored vector geometry (see static/logo.svg), so these
 * tests guard the two things that silently break: the in-app mark losing its
 * theme colour, and the static asset files going missing from the build output.
 */test.describe('Branding', () => {
	test('sidebar renders the mark beside the app name', async ({ page }) => {
		const brand = page.locator('.sidebar-header .app-brand');
		await expect(brand).toBeVisible();
		await expect(brand.locator('.app-name')).toHaveText('Glyph');

		const logo = brand.locator('svg.app-logo');
		await expect(logo).toBeVisible();
		await expect(logo).toHaveAttribute('viewBox', '0 0 64 64');

		// The mark is two stroked paths: the quiet note lines, and the third
		// stroke that begins as a line and resolves into a checkmark.
		await expect(logo.locator('path')).toHaveCount(2);

		// It must inherit the --accent token rather than hardcode a colour,
		// so the stroke (currentColor) tracks the theme.
		await expect(logo).toHaveCSS('color', 'rgb(47, 184, 160)');
	});

	test('accent and its on-accent foreground meet WCAG AA', async ({ page }) => {
		const { accent, contrast } = await page.evaluate(() => {
			const s = getComputedStyle(document.documentElement);
			return {
				accent: s.getPropertyValue('--accent').trim(),
				contrast: s.getPropertyValue('--accent-contrast').trim()
			};
		});
		expect(accent).toBeTruthy();
		expect(contrast).toBeTruthy();

		// The accent is a high-luminance teal, so white text on it would only
		// reach ~2.5:1. --accent-contrast exists to prevent that regression.
		const ratio = await page.evaluate(
			([a, c]) => {
				const lum = (hex: string) => {
					const h = hex.replace('#', '');
					const ch = [0, 2, 4]
						.map((i) => parseInt(h.slice(i, i + 2), 16) / 255)
						.map((x) => (x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4));
					return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2];
				};
				const [l1, l2] = [lum(a), lum(c)].sort((x, y) => y - x);
				return (l1 + 0.05) / (l2 + 0.05);
			},
			[accent, contrast]
		);
		expect(ratio).toBeGreaterThanOrEqual(4.5);
	});

	test('declares an SVG favicon and an apple touch icon', async ({ page }) => {
		await expect(page.locator('link[rel="icon"]')).toHaveAttribute('href', /favicon\.svg$/);
		await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveAttribute(
			'href',
			/apple-touch-icon\.png$/
		);
	});

	test('serves every brand asset', async ({ page, baseURL }) => {
		const assets: Array<[string, string]> = [
			['favicon.svg', 'image/svg+xml'],
			['logo.svg', 'image/svg+xml'],
			['logo-mono.svg', 'image/svg+xml'],
			['logo-wordmark.svg', 'image/svg+xml'],
			['logo-wordmark-mono.svg', 'image/svg+xml'],
			['apple-touch-icon.png', 'image/png'],
			['icon-192.png', 'image/png'],
			['icon-512.png', 'image/png']
		];

		for (const [asset, contentType] of assets) {
			const res = await page.request.get(`${baseURL}/${asset}`);
			expect(res.status(), `${asset} should be served`).toBe(200);
			expect(res.headers()['content-type'], `${asset} content-type`).toContain(contentType);
		}
	});

	test('vector sources are valid SVG with no raster fallback', async ({ page, baseURL }) => {
		for (const asset of ['logo.svg', 'favicon.svg', 'logo-wordmark.svg']) {
			const body = await (await page.request.get(`${baseURL}/${asset}`)).text();
			expect(body, `${asset} should be an <svg> document`).toContain('<svg');
			expect(body).toContain('viewBox');
			// Guard against anyone "fixing" the vector art by embedding a bitmap.
			expect(body, `${asset} must stay pure vector`).not.toContain('<image');
			expect(body).not.toContain('base64');
		}
	});
});
