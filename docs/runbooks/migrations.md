# Runbook: database migrations

How schema migrations run in the Argo CD deployment, what the failure states
look like, and how to recover from them.

Production is the Argo CD Application `glyph` (homelab repo,
`kubernetes/argocd/glyph/`). CD (`scripts/argocd-deploy.sh`) points it at a
commit of this repo for the chart and at that commit's tree hash for the
images, then waits for the sync and the rollout. PR previews are separate
Applications in the `glyph-preview` namespace with their own databases;
nothing here applies to them except the Job mechanics.

## How migrations run

- Migrations live in `api/migrations` (golang-migrate, one transaction per
  file). The chart reads them through `helm/glyph/migrations`, a symlink.
- **One place runs them:** the `glyph-migrate` Job, which runs
  `helm/glyph/files/glyph-migrate.sh apply`.
  - Under Argo CD (`argocd.enabled: true`) it is a `Sync` hook in sync wave
    -1, so it runs on every sync: after the CNPG Cluster and the
    `glyph-migrations` ConfigMap (wave -2) and before any Deployment changes
    (wave 0). If it fails, the sync fails and the Deployments are not touched
    (see [No automatic rollback](#no-automatic-rollback)). It is not
    `PreSync` because on a first sync the database it waits for is only
    created during the Sync phase.
  - With plain Helm (`argocd.enabled: false`, e.g. `scripts/test-e2e-k8s.sh`)
    it is a `pre-upgrade` hook named `glyph-migrate-<revision>` that reads
    the `glyph-migrations-hook` ConfigMap, and an ordinary Job on first
    install.
- **API pods never migrate.** Their `wait-for-schema` init container
  (`glyph-migrate.sh wait`) only waits until the schema is at least the
  release's latest migration. A pod that is waiting shows `Init:1/2`; it
  does not crash-loop.
- If the schema is **newer** than the release's files (after a rollback,
  or when an older tree is deployed), both modes log a warning and
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
- The Argo CD sync of the commit ends `Failed` and the CD deploy job fails
  with it. The Deployments were never changed, so the old pods keep serving.
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
   kubectl -n glyph logs job/glyph-migrate -c migrate
   kubectl -n argocd get application glyph -o jsonpath='{.status.operationState.message}' 
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
   anything after it. (Argo CD doesn't retry a failed sync of the same
   commit on its own; the deploy script requests one.)

## Failure: the schema is ahead of the release

**Symptoms.** The log says `WARNING: schema version N is newer than this
release's latest migration (M)`.

**What it means.** The release is older than the database. This happens
after a rollback past a commit whose migration had already applied,
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

Nothing rolls production back on its own: CD and `scripts/deploy.sh` run
`scripts/argocd-deploy.sh`, which fails when the sync or the rollout fails
and does nothing else. Tests in `helm/glyph/tests/cd_test.sh`,
`argocd_deploy_test.sh` and `deploy_test.sh` hold this.

**Why.** By the time a release can fail its rollout, the migrate hook has
already applied the new migrations. Every chart revision from before PR #46
has a `run-migrations` init container that runs a plain `migrate up` with its
own, older files; against the newer schema that fails with `no migration
found for version N`, so its API pods crash-loop. Those revisions are
already shipped and can't be changed. Revisions from PR #46 on (every commit
Argo CD has ever deployed) use `wait-for-schema`, which starts on a schema
that is ahead of it, so rolling back between those is safe, but only a
person should decide to.

**What a failed release looks like.** The deploy job fails and the
Application shows the sync `Failed` or the app `Degraded`. The Deployments
may have the new pod template, but a RollingUpdate only retires old pods as
new ones become ready, so the **old ReplicaSet's pods keep serving**. That is
the safe state; nothing is urgent.

### Recover from a failed release

1. See what failed.
   ```sh
   kubectl -n argocd get application glyph -o jsonpath='{.status.operationState.phase}: {.status.operationState.message}{"\n"}'
   kubectl -n glyph get pods
   kubectl -n glyph rollout status deploy/glyph-api --timeout 5s
   kubectl -n glyph describe pod <new-pod>     # or: logs <new-pod>
   ```
   (or the Argo CD UI at https://argoci.kurpuis.com/applications/glyph).
2. **Roll forward (preferred).** Fix the cause on `main` and redeploy
   (`gh workflow run cd.yml --ref main`). If it was transient (an image pull,
   the timeout), just re-run the deploy.
3. **Roll back** by pointing the Application at an earlier commit of `main`
   that is from PR #46 on (check that its `helm/glyph/templates/
   api-deployment.yaml` has `wait-for-schema` and no `run-migrations`):
   ```sh
   git fetch origin && sha=<commit>
   scripts/argocd-deploy.sh "$sha" "$(git rev-parse "$sha^{tree}")"
   ```
   The next CD run moves it forward again. Argo CD's own rollback is
   disabled while automated sync is on; don't turn sync off to use it.

## Failure: a sync is stuck

A sync that waits forever (usually the migrate hook waiting on a database
that never becomes ready) blocks every later deploy. See what it waits on,
fix that, or end the operation:

```sh
kubectl -n argocd get application glyph -o jsonpath='{.status.operationState.message}'
kubectl -n glyph get jobs,pods -l app.kubernetes.io/component=migrate
argocd app terminate-op glyph     # or "Terminate" on the sync in the UI
```

Terminating leaves whatever already applied in place, the same as a failed
sync. Then redeploy through CD.

## Manual deploys

`scripts/deploy.sh` is the fallback when CD can't run. It deploys only the
clean tip of `origin/main`, refuses while a CD run is active, builds any
missing tree-hash image, and then deploys exactly as CD does, with
`scripts/argocd-deploy.sh` and no automatic rollback. Prefer
`gh workflow run cd.yml --ref main`. Never `helm upgrade` production by hand:
Argo CD owns the release and its self-heal reverts it.
