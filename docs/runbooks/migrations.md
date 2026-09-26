# Runbook: database migrations

How schema migrations run in the Helm deployment, what the failure states
look like, and how to recover from them.

## How migrations run

- Migrations live in `api/migrations` (golang-migrate, one transaction per
  file). CD copies them into `helm/glyph/migrations` at deploy time.
- **One place runs them:** the `glyph-migrate-<revision>` Job, which runs
  `helm/glyph/files/glyph-migrate.sh apply`.
  - On `helm upgrade` it is a `pre-upgrade` hook. It finishes before any
    Deployment changes. If it fails, the upgrade fails, and `--atomic` rolls
    the release back. The Job reads this release's migrations from the
    `glyph-migrations-hook` ConfigMap.
  - On first install it is an ordinary Job, because there is no database
    yet for a pre-install hook to wait on.
- **API pods never migrate.** Their `wait-for-schema` init container
  (`glyph-migrate.sh wait`) only waits until the schema is at least the
  release's latest migration. A pod that is waiting shows `Init:1/2`; it
  does not crash-loop.
- If the schema is **newer** than the release's files (after a `helm
  rollback`, or when an older tree is deployed), both modes log a warning and
  carry on. `migrate up` would otherwise fail with `no migration found for
  version N`.

### Rules for writing migrations (expand/contract)

The migrate hook runs while the **previous** release's pods are still
serving, and a rollback runs the previous release against the **new**
schema. So every migration must be compatible with the code on both sides of
it:

1. **Expand:** add columns and tables as nullable or with defaults, and add
   indexes `CONCURRENTLY` in a migration of their own. Old code must keep
   working.
2. Ship the code that uses the new shape.
3. **Contract** (drop or rename old columns, tighten constraints) only in a
   later release, once no running or rollback-target release reads them.

Never rename a column in place. Never make a column `NOT NULL` in the same
release that starts writing it.

## Failure: the schema is dirty

**Symptoms**
- The migrate Job fails, and its log says `schema version N is dirty`, or
  `migrate up` fails with an SQL error.
- `helm upgrade` fails and `--atomic` rolls the release back. The old pods
  keep serving.
- New API pods wait in `Init:1/2`, logging `schema version N is dirty`.

**What it means.** Migration `N` started and did not finish. golang-migrate
set `dirty = true` in `schema_migrations` and will refuse to run anything
until a person clears it. Each file runs in one transaction, so a failed
migration usually rolled back completely. Check before assuming that,
because statements such as `CREATE INDEX CONCURRENTLY` run outside the
transaction.

**Recover**

1. Read the failure.
   ```sh
   kubectl -n glyph get jobs -l app.kubernetes.io/component=migrate
   kubectl -n glyph logs job/glyph-migrate-<revision> -c migrate
   ```
2. Open a psql shell on the primary.
   ```sh
   kubectl -n glyph exec -it glyph-db-1 -- psql -U postgres glyph
   # or: kubectl cnpg psql glyph-db -n glyph -- glyph
   ```
   Then check the state:
   ```sql
   SELECT version, dirty FROM schema_migrations;
   ```
3. Work out how much of `api/migrations/0000NN_*.up.sql` was applied. Look
   at the objects it creates or changes (`\d table`, `\di`).
4. Get the schema to one of the two clean states:
   - **None of N applied** (the usual case): mark the previous version as
     clean.
     ```sh
     kubectl -n glyph run migrate-force --rm -it --restart=Never \
       --image=migrate/migrate:v4.17.0 \
       --env="DATABASE_URL=$(kubectl -n glyph get secret glyph-db-app -o jsonpath='{.data.uri}' | base64 -d)" \
       --command -- sh -c 'migrate -path /tmp -database "$DATABASE_URL" force <N-1>'
     ```
   - **All of N applied**: run `force N` instead.
   - **Partly applied**: undo the partial changes by hand in psql, then run
     `force <N-1>`.
5. Fix the migration, which is often a data problem the migration didn't
   expect. Examples: legacy `NULL`s, or duplicates that block a unique
   index. Merge the fix. Don't edit a migration that has already applied
   cleanly anywhere; add a new one.
6. Redeploy (`gh workflow run cd.yml --ref main`). The hook applies N and
   anything after it.

## Failure: the schema is ahead of the release

**Symptoms.** The log says `WARNING: schema version N is newer than this
release's latest migration (M)`.

**What it means.** The release is older than the database. This happens
after `helm rollback`, after an `--atomic` rollback of an upgrade whose
migration had already applied, or when an older tree is deployed. Nothing is
migrated. The pods start, which is safe as long as the migrations followed
expand/contract.

**Act** only if the older code can't work against the newer schema (a
contract step shipped too early). In that case, roll forward, or apply the
matching `.down.sql` by hand after reviewing it, then `force` the lower
version. Down migrations are not run by any automation. Several of them
destroy data (see the audit, "Destructive or failing down migrations"), so
read them first.

## Failure: the upgrade is stuck in `pending-upgrade`

An interrupted `helm upgrade` can leave the release locked:

```sh
helm -n glyph history glyph
helm -n glyph rollback glyph <last-deployed-revision>
```

Then redeploy through CD.

## Manual deploys

`scripts/deploy.sh` is the fallback when CD can't run. It deploys only the
clean tip of `origin/main`, refuses while a CD run is active, uses the same
tree-hash image tags as CD, and upgrades with `--atomic --wait`. Prefer
`gh workflow run cd.yml --ref main`.
