#!/usr/bin/env node
// Enroll (or rotate) the software passkey of the `glyph-agent` Pocket ID
// user, the identity agents use to sign in to PR previews
// (scripts/preview-login.mjs). Run by an operator, not by agents.
//
//   node scripts/preview-agent-enroll.mjs --context <homelab kube context> [--replace]
//
// 1. Mints a one-time sign-in code for glyph-agent with Pocket ID's admin API
//    (STATIC_API_KEY, read from the pocket-id-secrets Secret) and redeems it
//    in a headless Chromium with a virtual authenticator.
// 2. Registers a new passkey on that authenticator, exports it (private key
//    included), and writes it to controller-agent/glyph-agent-passkey
//    (key credential.json), which claude-code-swe-agent receives as
//    GLYPH_AGENT_PASSKEY.
// 3. With --replace, deletes glyph-agent's other passkeys once the new one
//    is stored, so the old credential stops working. Without it, the script
//    refuses if glyph-agent already has a passkey.
//
// Nothing secret is printed. Needs kubectl access to the homelab cluster
// and Chromium (CHROME_BIN, or Playwright's).
import { chromium } from '@playwright/test';
import { execFileSync } from 'node:child_process';

const POCKET_ID = 'https://pocket-id.kurpuis.com';
const AGENT = 'glyph-agent';
const SECRET_NS = 'controller-agent';
const SECRET = 'glyph-agent-passkey';

const args = process.argv.slice(2);
const ctxIdx = args.indexOf('--context');
const context = ctxIdx >= 0 ? args[ctxIdx + 1] : null;
const replace = args.includes('--replace');
if (!context) {
  console.error('usage: node scripts/preview-agent-enroll.mjs --context <homelab kube context> [--replace]');
  process.exit(2);
}

const kubectl = (a, input) => execFileSync('kubectl', ['--context', context, ...a],
  { encoding: 'utf8', input, stdio: [input === undefined ? 'ignore' : 'pipe', 'pipe', 'inherit'] });
const apiKey = Buffer.from(kubectl(['-n', 'pocket-id', 'get', 'secret', 'pocket-id-secrets',
  '-o', 'jsonpath={.data.STATIC_API_KEY}']), 'base64').toString();

async function admin(method, path, body) {
  const res = await fetch(`${POCKET_ID}/api/${path}`, {
    method,
    headers: { 'X-API-Key': apiKey, 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} /api/${path}: ${res.status} ${text.slice(0, 200)}`);
  return text ? JSON.parse(text) : null;
}

async function main() {
  const users = await admin('GET', `users?search=${AGENT}&pagination%5Blimit%5D=100`);
  const user = users.data.find(u => u.username === AGENT);
  if (!user) throw new Error(`Pocket ID has no ${AGENT} user (run homelab's pocket-id/provision-glyph.sh)`);
  const existing = await admin('GET', `users/${user.id}/webauthn-credentials`);
  if (existing.length && !replace) {
    throw new Error(`${AGENT} already has ${existing.length} passkey(s); pass --replace to rotate`);
  }

  const { token } = await admin('POST', `users/${user.id}/one-time-access-token`, { ttl: '10m' });
  const browser = await chromium.launch({
    headless: true,
    ...(process.env.CHROME_BIN ? { executablePath: process.env.CHROME_BIN } : {}),
  });
  try {
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    const cdp = await ctx.newCDPSession(page);
    await cdp.send('WebAuthn.enable');
    const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
      options: {
        protocol: 'ctap2', transport: 'internal', hasResidentKey: true,
        hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true,
      },
    });

    // Redeem the code the way Pocket ID's own login-code form does. (That
    // form never submits in headless Chromium, so call its endpoint directly
    // from the page, which sets the session cookie.)
    await page.goto(`${POCKET_ID}/login`, { waitUntil: 'domcontentloaded' });
    const status = await page.evaluate(async t => (await fetch(`/api/one-time-access-token/${t}`,
      { method: 'POST', credentials: 'include' })).status, token);
    if (status !== 200) throw new Error(`redeeming the one-time code returned ${status}`);

    await page.goto(`${POCKET_ID}/settings/account`, { waitUntil: 'networkidle' });
    await page.getByRole('button', { name: 'Add Passkey' }).first().click();
    let credentials = [];
    for (let i = 0; i < 40 && credentials.length === 0; i++) {
      await page.waitForTimeout(500);
      ({ credentials } = await cdp.send('WebAuthn.getCredentials', { authenticatorId }));
    }
    if (credentials.length !== 1) throw new Error(`expected 1 new credential, got ${credentials.length}`);
    await page.waitForLoadState('networkidle');

    const after = await admin('GET', `users/${user.id}/webauthn-credentials`);
    const newOnes = after.filter(c => !existing.some(e => e.id === c.id));
    if (newOnes.length !== 1) throw new Error(`Pocket ID shows ${newOnes.length} new passkeys, expected 1`);

    // Store it (create or replace), never printing it.
    const secret = {
      apiVersion: 'v1', kind: 'Secret', type: 'Opaque',
      metadata: {
        name: SECRET, namespace: SECRET_NS,
        labels: { 'app.kubernetes.io/part-of': 'glyph-previews' },
        annotations: { 'glyph.kurpuis.com/purpose': 'Passkey of the Pocket ID user glyph-agent; signs claude-code-swe-agent in to Glyph PR previews (GLYPH_AGENT_PASSKEY). Rotate: glyph scripts/preview-agent-enroll.mjs --replace.' },
      },
      stringData: { 'credential.json': JSON.stringify({ ...credentials[0], pocketIdUser: AGENT, enrolledAt: new Date().toISOString() }) },
    };
    // Server-side apply: client-side apply would copy the private key into the
    // last-applied-configuration annotation, which describe/get -o yaml print.
    kubectl(['apply', '--server-side', '--force-conflicts', '--field-manager=preview-agent-enroll', '-f', '-'], JSON.stringify(secret));
    console.error(`enrolled a passkey for ${AGENT}; stored in ${SECRET_NS}/${SECRET}[credential.json]`);

    if (replace) {
      for (const c of existing) {
        await admin('DELETE', `users/${user.id}/webauthn-credentials/${c.id}`);
      }
      if (existing.length) console.error(`deleted ${existing.length} old passkey(s)`);
    }
  } finally {
    await browser.close();
  }
}

main().catch(err => {
  console.error(`preview-agent-enroll: ${err.message.split('\n')[0]}`);
  process.exit(1);
});
