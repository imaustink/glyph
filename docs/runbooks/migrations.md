# Runbook: database migrations

How schema migrations run in the Helm deployment, what the failure states
look like, and how to recover from them.

## How migrations run

- Migrations live in `api/migrations` (golang-migrate, one transaction per
  file). CD copies them into `helm/glyph/migrations` at deploy time.
- **One place runs them:** the `glyph-migrate-<revision>` Job, which runs
  `helm/glyph/files/glyph-migrate.sh apply`.
  - On `helm upgrade` it is a `pre-upgrade` hook. It finishes before any
    Deployment changes. If it fails, the upgrade fails and nothing else
    changes (see [No automatic rollback](#no-automatic-rollback)). The Job reads this release's migrations from the
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
- `helm upgrade` fails and the release is marked `failed`. The Deployments
  were never changed, so the old pods keep serving.
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
after a `helm rollback` of an upgrade whose migration had already applied,
or when an older tree is deployed. Nothing is
migrated. The pods start, which is safe as long as the migrations followed
expand/contract.

**Act** only if the older code can't work against the newer schema (a
contract step shipped too early). In that case, roll forward, or apply the
matching `.down.sql` by hand after reviewing it, then `force` the lower
version. Down migrations are not run by any automation. Several of them
destroy data (see the audit, "Destructive or failing down migrations"), so
read them first.

## No automatic rollback

CD and `scripts/deploy.sh` run `helm upgrade --wait --cleanup-on-fail
--timeout 10m`, deliberately **without `--atomic`** (Helm 4:
`--rollback-on-failure`). Tests in `helm/glyph/tests/cd_test.sh` and
`deploy_test.sh` hold this.

**Why.** By the time a release can fail its rollout, the pre-upgrade hook has
already applied the new migrations. Rolling back puts the previous
revision's API pod template back. Every revision from before PR #46 has a
`run-migrations` init container that runs a plain `migrate up` with its own,
older files. Against the newer schema that fails with `no migration found
for version N`, so every API pod that revision has to create (during the
rollback, or later on a reschedule, eviction or scale-up) crash-loops.
Those revisions are already shipped and can't be changed.

Revisions from PR #46 on use `wait-for-schema` (`glyph-migrate.sh wait`),
which starts on a schema that is ahead of it. Rolling back between those is
safe. Helm can't tell the two kinds apart, so the pipeline never rolls back
on its own; a person does it, after checking.

**What a failed release looks like.** The deploy job fails and `helm -n glyph
history glyph` shows the newest revision as `failed`. With no rollback, the
Deployments keep the new pod template, but a RollingUpdate only retires old
pods as new ones become ready, so the **old ReplicaSet's pods keep
serving**. That is the safe state; nothing is urgent. `--cleanup-on-fail`
only deletes objects this upgrade newly created, which the old pods don't
use. The next `helm upgrade` works from a `failed` release.

### Recover from a failed release

1. See what failed.
   ```sh
   helm -n glyph history glyph
   kubectl -n glyph get pods
   kubectl -n glyph rollout status deploy/glyph-api --timeout 5s
   kubectl -n glyph describe pod <new-pod>     # or: logs <new-pod>
   ```
2. **Roll forward (preferred).** Fix the cause on `main` and redeploy
   (`gh workflow run cd.yml --ref main`). If it was transient (an image pull,
   the timeout), just re-run the deploy.
3. **Roll back only to a schema-tolerant revision.** First check that the
   target revision's API Deployment waits for the schema instead of
   migrating:
   ```sh
   helm -n glyph get manifest glyph --revision <N> | grep -c 'name: wait-for-schema'   # must be >= 1
   helm -n glyph get manifest glyph --revision <N> | grep -c 'name: run-migrations'    # must be 0
   helm -n glyph rollback glyph <N> --wait --timeout 10m
   ```
   If no deployed revision passes that check (the first deploy after PR #46
   is the only one with a pre-PR predecessor), **don't roll back**. Leave the
   old pods serving and roll forward. `kubectl rollout undo` is no
   different: it puts the same old pod template back.
4. **If a pre-PR revision was rolled back to anyway** and API pods
   crash-loop in `run-migrations` with `no migration found for version N`:
   roll forward to the tip of `main` (step 2); its pods start on the newer
   schema. Pods of the old revision that were already running keep serving
   until then. Don't `force` the schema version down to make the old
   init container happy; that marks applied migrations as not applied.

## Failure: the upgrade is stuck in `pending-upgrade`

An interrupted `helm upgrade` can leave the release locked:

```sh
helm -n glyph history glyph
```

If the last `deployed` revision passes the schema-tolerance check in
[Recover from a failed release](#recover-from-a-failed-release), step 3,
roll back to it:

```sh
helm -n glyph rollback glyph <last-deployed-revision>
```

If it doesn't (it is from before PR #46), rolling back would crash-loop the
API. Delete the stuck revision's release record instead. That releases the
lock without changing any workload (Helm reads the release status from the
record itself, so relabeling it is not enough):

```sh
kubectl -n glyph get secret -l owner=helm,name=glyph   # find sh.helm.release.v1.glyph.v<pending-revision>
kubectl -n glyph delete secret sh.helm.release.v1.glyph.v<pending-revision>
```

Then redeploy through CD.

## Manual deploys

`scripts/deploy.sh` is the fallback when CD can't run. It deploys only the
clean tip of `origin/main`, refuses while a CD run is active, uses the same
tree-hash image tags as CD, and upgrades with `--wait` and no automatic
rollback, like CD (see [No automatic rollback](#no-automatic-rollback)). Prefer
`gh workflow run cd.yml --ref main`.
