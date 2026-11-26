---
name: glyph-preview-login
description: Sign in to a Glyph PR preview environment (https://pr-<n>.glyph-preview.kurpuis.com) as the glyph-agent user, to test or screenshot a PR's change in the real deployed app while logged in. Use when verifying a Glyph pull request in its preview, or whenever a preview page needs an authenticated session.
---

# Signing in to a Glyph PR preview

Every open same-repo Glyph PR labeled `preview` gets its own deployment at
`https://pr-<number>.glyph-preview.kurpuis.com`, with its own database seeded
with a "Glyph Demo" workspace (notes, tasks, boards, templates). Sign-in is
Pocket ID, which uses passkeys only. You sign in as the `glyph-agent` user
with a software passkey.

## Sign in

From the root of a glyph checkout, after `pnpm install`:

```sh
node scripts/preview-login.mjs <pr-number>
```

On success it prints, on stderr, who you're signed in as, and saves a
Playwright storageState to `.preview-auth/pr-<n>.json`. That file is
gitignored and readable only by you. Then reuse the session:

```js
const context = await browser.newContext({ storageState: '.preview-auth/pr-123.json' });
```

For the API or curl, add `--print-cookie-header`, which prints
`Cookie: glyph_session=...` on stdout. Writes under `/api/v1` (anything
but GET, HEAD or OPTIONS) also need an `X-Requested-With` header with any
value, or they get 403 "missing CSRF header":

```sh
curl -H "$(node scripts/preview-login.mjs 123 --print-cookie-header)" \
  -H 'X-Requested-With: fetch' https://pr-123.glyph-preview.kurpuis.com/api/v1/...
```

It needs `GLYPH_AGENT_PASSKEY`, the passkey as JSON, which the agent
environment provides. Without it, say so and stop; don't try another way in.
It uses the system Chromium (`CHROME_BIN`); only Chromium supports the
virtual authenticator it relies on.

## Before you sign in

- **Is the preview up?** It comes up a few minutes after CI passes for the
  PR's latest push. Check with
  `curl -s -o /dev/null -w '%{http_code}' https://pr-<n>.glyph-preview.kurpuis.com/`.
  A 404 or TLS error means it isn't deployed (yet), or the PR isn't labeled
  `preview`.
- **Which commit?** The preview runs the PR's head commit. After a push, wait
  for CI before trusting what you see.

## Rules

- **Previews only.** glyph-agent can't sign in to production
  (glyph.kurpuis.com), and you must not try.
- **Treat the session and the passkey as secrets.** Never print
  `GLYPH_AGENT_PASSKEY`, the storageState file, or the cookie into logs, PR
  comments or chat, and never commit `.preview-auth/`.
- **Data is disposable.** Each preview's database is deleted with the
  preview. Edit freely, but anything you create exists only in that preview.
- **Failures:**
  - "not allowed to access this service": glyph-agent isn't in the Pocket ID
    `glyph-preview-users` group.
  - Stuck at Pocket ID's login page: the passkey was revoked or rotated. An
    operator re-enrolls it with `scripts/preview-agent-enroll.mjs`.
  - Report either; don't work around it.
