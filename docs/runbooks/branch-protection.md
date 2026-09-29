# Runbook: require branches to be up to date before merging

Why the default branch must require up-to-date branches before a merge, what
setting enforces it, and the exact command to apply it.

## Why this matters

Container images are tagged by **git tree hash**, not commit SHA
(`git rev-parse HEAD^{tree}` — see `ci.yml`'s `build-and-push` and `cd.yml`'s
`build-missing`). `ci.yml` builds and pushes an image tagged with the
**PR-head tree**; `cd.yml` deploys the image whose tag is the **merge-commit
tree** that lands on `main`.

- When a PR branch is **up to date** with `main`, the head already contains
  `main`, so merging is fast-forward-equivalent: the merge-commit tree equals
  the PR-head tree CI built, and the prebuilt image matches.
- When a PR branch is **behind** `main`, GitHub writes a real merge commit
  whose tree **differs** from the PR-head tree CI built. The tip-of-`main`
  tree then has **no matching image**, so a fresh push/CD run has to rebuild
  it (via `build-missing`), and the tip image can be missing until it does.

Merging three behind-`main` PRs back to back is exactly the situation that
produced the missing tip-of-`main` image this runbook exists to prevent.
`cd.yml`'s `build-missing` job self-heals the common case, and the manual
`publish-images.yml` workflow (Actions → **Publish images**) is the on-demand
escape hatch — but the durable fix is to stop the mismatch from ever being
mergeable.

## The fix

Enable **"Require branches to be up to date before merging"** on the `main`
branch. In ruleset terms this is a `required_status_checks` rule with
`strict_required_status_checks_policy: true`. GitHub blocks the merge until the
PR branch has been updated to include the latest `main`, which forces CI to
re-run against a head that already contains `main`. That head's tree is the
tree that lands on `main`, so the image CI built always matches what CD
deploys.

This is added to the existing **"Protect main"** ruleset (repository ruleset
id `17646072`, `~DEFAULT_BRANCH`), alongside its current `deletion`,
`non_fast_forward`, and `pull_request` rules.

### Which checks to require

Require the six PR checks that run on every PR (including fork PRs):

- `frontend`
- `collab`
- `api`
- `helm`
- `e2e-local`
- `e2e-api`

Do **not** require `build-and-push` (or its matrix contexts
`build-and-push (frontend|api|collab)`): that job is intentionally skipped on
fork PRs (`ci.yml` skips it when there are no registry secrets), so requiring
it would leave fork PRs permanently unmergeable. The six checks above are
enough to guarantee the branch is up to date and green before merge; the
tree-hash image for that up-to-date head is then built by `build-and-push`
(non-fork PRs) or backfilled by `cd.yml`'s `build-missing` after merge.

## Applying it

Requires a token/user with **admin** on `imaustink/glyph`
(`Administration: write`). This cannot be applied by CI's default token.

### Option A — GitHub UI

**Settings → Rules → Rulesets → Protect main → Add rule → Require status
checks to pass** → tick **Require branches to be up to date before merging** →
add the six status checks listed above → **Save changes**.

### Option B — API (copy-paste)

Replaces the ruleset's `rules` with the current three rules plus the new
`required_status_checks` rule. Review, then run:

```sh
gh api --method PUT repos/imaustink/glyph/rulesets/17646072 \
  --input - <<'JSON'
{
  "name": "Protect main",
  "target": "branch",
  "enforcement": "active",
  "conditions": { "ref_name": { "include": ["~DEFAULT_BRANCH"], "exclude": [] } },
  "rules": [
    { "type": "deletion" },
    { "type": "non_fast_forward" },
    {
      "type": "pull_request",
      "parameters": {
        "required_approving_review_count": 0,
        "dismiss_stale_reviews_on_push": false,
        "required_reviewers": [],
        "require_code_owner_review": false,
        "require_last_push_approval": false,
        "required_review_thread_resolution": true,
        "require_extra_approval_for_unattributed_changes": true,
        "allowed_merge_methods": ["merge", "squash", "rebase"]
      }
    },
    {
      "type": "required_status_checks",
      "parameters": {
        "strict_required_status_checks_policy": true,
        "do_not_enforce_on_create": false,
        "required_status_checks": [
          { "context": "frontend" },
          { "context": "collab" },
          { "context": "api" },
          { "context": "helm" },
          { "context": "e2e-local" },
          { "context": "e2e-api" }
        ]
      }
    }
  ]
}
JSON
```

### Verify

```sh
gh api repos/imaustink/glyph/rulesets/17646072 \
  --jq '.rules[] | select(.type=="required_status_checks") | .parameters.strict_required_status_checks_policy'
# -> true
```

After this is in place, a PR that has fallen behind `main` shows **"This
branch is out-of-date"** and must be updated (which re-runs CI) before the
merge button is enabled — closing the tree-mismatch gap at the source.
