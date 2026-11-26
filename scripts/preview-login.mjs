#!/usr/bin/env node
// Sign the `glyph-agent` Pocket ID user in to a PR preview environment, with
// no human in the loop, and save the logged-in browser state for reuse.
//
//   node scripts/preview-login.mjs <pr-number | preview URL> [--out FILE]
//
// Writes a Playwright storageState file (default: .preview-auth/pr-<n>.json at
// the repo root, gitignored) holding the preview's session cookie. Reuse it with
//   browser.newContext({ storageState: FILE })     (Playwright)
// or pass the cookie to curl / the API (see --print-cookie-header).
//
// How: glyph-agent has a passkey (a WebAuthn credential, private key
// included) stored as a Kubernetes Secret and handed to the agent as
// GLYPH_AGENT_PASSKEY (JSON; or a file path in GLYPH_AGENT_PASSKEY_FILE). This
// script loads it into a Chrome DevTools virtual authenticator, starts the
// preview's normal login, and lets Pocket ID authenticate the passkey. Only
// Chromium has virtual authenticators.
//
// glyph-agent can sign in to previews only: Pocket ID's `glyph-preview` client
// allows the glyph-preview-users group; production's `glyph` client refuses
// it. Each preview has its own database, so glyph-agent is a new user in each
// one; the preview's demo-data seed makes it an editor of the "Glyph Demo"
// workspace.
//
// Options:
//   --out FILE              where to write the storageState
//   --print-cookie-header   also print `Cookie: glyph_session=...` to stdout
//                           (secret: it is a live session for that preview)
//   --headed                show the browser (local debugging)
//
// Env: GLYPH_AGENT_PASSKEY / GLYPH_AGENT_PASSKEY_FILE (required),
//      CHROME_BIN (system Chromium; the agent image sets it).
import { chromium } from '@playwright/test';
import { mkdirSync, readFileSync, writeFileSync, chmodSync } from 'node:fs';
import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// Hard-coded on purpose: this is the previews-only guard, so the environment
// must not be able to widen it.
const DOMAIN = 'glyph-preview.kurpuis.com';
const POCKET_ID = 'https://pocket-id.kurpuis.com';

function usage(msg) {
  if (msg) console.error(`preview-login: ${msg}`);
  console.error('usage: node scripts/preview-login.mjs <pr-number | preview URL> [--out FILE] [--print-cookie-header] [--headed]');
  process.exit(2);
}

function parseArgs(argv) {
  const opts = { out: null, printCookie: false, headed: false, target: null };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--out') opts.out = argv[++i] ?? usage('--out needs a value');
    else if (a === '--print-cookie-header') opts.printCookie = true;
    else if (a === '--headed') opts.headed = true;
    else if (a === '-h' || a === '--help') usage();
    else if (!opts.target) opts.target = a;
    else usage(`unexpected argument ${a}`);
  }
  if (!opts.target) usage('which preview?');
  let origin;
  if (/^\d+$/.test(opts.target)) origin = `https://pr-${opts.target}.${DOMAIN}`;
  else {
    try { origin = new URL(opts.target).origin; } catch { usage(`not a PR number or URL: ${opts.target}`); }
  }
  const host = new URL(origin).hostname;
  if (!host.endsWith(`.${DOMAIN}`)) usage(`${origin} is not a preview (*.${DOMAIN})`);
  opts.origin = origin;
  // Anchored to the repo root (gitignored there), not the cwd.
  opts.out ??= fileURLToPath(new URL(`../.preview-auth/${host.split('.')[0]}.json`, import.meta.url));
  return opts;
}

function loadPasskey() {
  const raw = process.env.GLYPH_AGENT_PASSKEY
    || (process.env.GLYPH_AGENT_PASSKEY_FILE && readFileSync(process.env.GLYPH_AGENT_PASSKEY_FILE, 'utf8'));
  if (!raw) {
    console.error('preview-login: GLYPH_AGENT_PASSKEY (or GLYPH_AGENT_PASSKEY_FILE) is not set');
    process.exit(2);
  }
  // JSON.parse's message quotes part of the input; never let it reach logs.
  let c;
  try { c = JSON.parse(raw); } catch { throw new Error('passkey is not valid JSON'); }
  for (const k of ['credentialId', 'privateKey', 'rpId', 'userHandle']) {
    if (!c[k]) throw new Error(`passkey JSON is missing ${k}`);
  }
  return c;
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  const passkey = loadPasskey();

  const browser = await chromium.launch({
    headless: !opts.headed,
    ...(process.env.CHROME_BIN ? { executablePath: process.env.CHROME_BIN } : {}),
  });
  try {
    const context = await browser.newContext();
    const page = await context.newPage();
    const cdp = await context.newCDPSession(page);
    await cdp.send('WebAuthn.enable');
    const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
      options: {
        protocol: 'ctap2', transport: 'internal', hasResidentKey: true,
        hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true,
      },
    });
    await cdp.send('WebAuthn.addCredential', {
      authenticatorId,
      credential: {
        credentialId: passkey.credentialId, isResidentCredential: true, rpId: passkey.rpId,
        privateKey: passkey.privateKey, userHandle: passkey.userHandle, signCount: passkey.signCount ?? 0,
      },
    });

    // The preview's own login: it redirects to Pocket ID, which wants the
    // passkey (its "Authenticate" button) unless a session already exists,
    // then sends the browser back to the preview's /auth/callback.
    await page.goto(`${opts.origin}/auth/login`);
    const back = u => u.origin === opts.origin && !u.pathname.startsWith('/auth/');
    const deadline = Date.now() + 60_000;
    while (!back(new URL(page.url()))) {
      if (Date.now() > deadline) {
        const where = new URL(page.url());
        const heading = (await page.locator('h1, h2').allInnerTexts().catch(() => [])).map(s => s.trim()).filter(Boolean);
        const buttons = (await page.getByRole('button').allInnerTexts().catch(() => [])).map(s => s.trim()).filter(Boolean);
        throw new Error(`stuck at ${where.origin}${where.pathname} (heading ${JSON.stringify(heading)}, buttons ${JSON.stringify(buttons)})`);
      }
      const u = new URL(page.url());
      if (u.origin === POCKET_ID) {
        const body = await page.locator('body').innerText().catch(() => '');
        if (/not allowed to access/i.test(body)) {
          throw new Error('Pocket ID refused: glyph-agent is not allowed to use this client (is it in glyph-preview-users?)');
        }
        // "Authenticate" asks for the passkey. "Sign in" is the account
        // confirmation Pocket ID shows on /interaction, because Glyph's login
        // sends prompt=select_account.
        for (const name of [/^Authenticate$/, /^Sign in$/]) {
          const btn = page.getByRole('button', { name });
          if (await btn.isVisible().catch(() => false)) {
            await btn.click();
            await page.waitForURL(x => x.pathname !== u.pathname || x.origin !== u.origin, { timeout: 20_000 }).catch(() => {});
            break;
          }
        }
        if (new URL(page.url()).pathname !== u.pathname) continue;
      }
      await page.waitForTimeout(500);
    }

    const me = await page.evaluate(async () => {
      const r = await fetch('/auth/me', { credentials: 'include' });
      return { status: r.status, body: r.ok ? await r.json() : null };
    });
    if (me.status !== 200) throw new Error(`signed in, but ${opts.origin}/auth/me returned ${me.status}`);

    mkdirSync(dirname(opts.out), { recursive: true });
    const state = await context.storageState();
    // Create it 0600 (no world-readable window); chmod covers an existing file.
    writeFileSync(opts.out, JSON.stringify(state, null, 2), { mode: 0o600 });
    chmodSync(opts.out, 0o600);
    console.error(`preview-login: signed in to ${opts.origin} as ${me.body.name} <${me.body.email}>; state saved to ${opts.out}`);

    if (opts.printCookie) {
      const session = state.cookies.find(c => c.name === 'glyph_session');
      if (!session) throw new Error('no glyph_session cookie after login');
      console.log(`Cookie: glyph_session=${session.value}`);
    }
  } finally {
    await browser.close();
  }
}

main().catch(err => {
  console.error(`preview-login: ${err.message.split('\n')[0]}`);
  process.exit(1);
});
