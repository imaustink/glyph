# shellcheck shell=bash
# DI-17 — one migration path: a pre-upgrade hook Job (a plain Job on first
# install, when there is no database yet for a hook to wait on); API pods only
# wait for the schema.

test_migrate_job_is_pre_upgrade_hook_on_upgrade() {
  render --is-upgrade
  assert_ok
  local j; j="$(doc Job glyph-migrate-1)"
  assert_contains "$j" 'kind: Job' "migrate Job rendered on upgrade"
  assert_contains "$j" '"helm.sh/hook": pre-upgrade' "runs before any Deployment is updated"
  assert_contains "$j" '"helm.sh/hook-delete-policy": before-hook-creation' "replaced on the next upgrade"
  assert_contains "$j" 'name: glyph-migrations-hook' "reads this release's migrations, not the previous ConfigMap"
  assert_contains "$j" 'glyph-migrate.sh", "apply"' "applies migrations via the chart's script"
  local cm; cm="$(doc ConfigMap glyph-migrations-hook)"
  assert_contains "$cm" '"helm.sh/hook": pre-upgrade' "hook ConfigMap exists before the Job"
  assert_contains "$cm" '"helm.sh/hook-weight": "-10"' "created before the Job"
  assert_contains "$cm" '000001_' "carries the migrations"
  assert_contains "$cm" 'glyph-migrate.sh:' "carries the script"
}

test_migrate_job_is_plain_job_on_install() {
  render
  assert_ok
  local j; j="$(doc Job glyph-migrate-1)"
  assert_contains "$j" 'kind: Job' "migrate Job rendered on install"
  assert_not_contains "$j" 'helm.sh/hook' "no hook on install: the database doesn't exist before the release"
  assert_contains "$j" 'name: glyph-migrations$' "uses the release ConfigMap"
  assert_not_contains "$OUT" 'name: glyph-migrations-hook' "no hook ConfigMap on install"
}

test_api_pods_wait_for_schema_but_never_migrate() {
  render --is-upgrade
  assert_ok
  local d; d="$(doc Deployment glyph-api)"
  assert_not_contains "$d" '- up$' "API init containers must not run migrate up"
  assert_not_contains "$d" 'run-migrations' "no per-pod migration init container"
  assert_contains "$d" 'name: wait-for-schema' "API waits for the schema instead"
  assert_contains "$d" 'glyph-migrate.sh", "wait"' "check-only mode"
}

# The script itself, against a fake `migrate` CLI.
#   fake_migrate VERSION_OUTPUT [EXIT] — `migrate version` prints VERSION_OUTPUT
#   on stderr and exits EXIT; every invocation is logged to $WORK/calls.
fake_migrate() {
  mkdir -p "$WORK/bin" "$WORK/migrations"
  for v in 1 2 3; do
    : >"$WORK/migrations/00000${v}_m${v}.up.sql"
    : >"$WORK/migrations/00000${v}_m${v}.down.sql"
  done
  printf '%s\n' "$1" >"$WORK/version"
  printf '%s\n' "${2:-0}" >"$WORK/version-exit"
  cat >"$WORK/bin/migrate" <<EOF
#!/bin/sh
echo "\$*" >>"$WORK/calls"
for a in "\$@"; do last="\$a"; done
case "\$last" in
  version) cat "$WORK/version" >&2; exit \$(cat "$WORK/version-exit") ;;
  up) [ -f "$WORK/up-fails" ] && { echo "error: boom" >&2; exit 1; }; echo "3" >"$WORK/version"; echo 0 >"$WORK/version-exit"; exit 0 ;;
esac
EOF
  chmod +x "$WORK/bin/migrate"
  : >"$WORK/calls"
}

# run_script MODE — runs files/glyph-migrate.sh; $OUT/$STATUS as for render.
run_script() {
  set +e
  OUT="$(PATH="$WORK/bin:$PATH" MIGRATIONS_DIR="$WORK/migrations" DATABASE_URL=postgres://x \
    WAIT_INTERVAL_SECONDS=0 WAIT_MAX_ATTEMPTS="${WAIT_MAX_ATTEMPTS:-3}" \
    sh "$CHART/files/glyph-migrate.sh" "$1" 2>&1)"
  STATUS=$?
  set -e
}

test_migrate_script_apply_runs_up_when_behind() {
  fake_migrate "1"
  run_script apply
  assert_ok
  assert_contains "$(cat "$WORK/calls")" ' up$' "runs migrate up"
}

test_migrate_script_apply_runs_up_on_fresh_database() {
  fake_migrate "error: no migration" 1
  run_script apply
  assert_ok
  assert_contains "$(cat "$WORK/calls")" ' up$' "runs migrate up on an empty database"
}

test_migrate_script_wait_returns_once_schema_is_current() {
  fake_migrate "3"
  run_script wait
  assert_ok
  assert_not_contains "$(cat "$WORK/calls")" ' up$' "wait never migrates"
}

# DI-18 — failure modes: a schema ahead of the shipped files (after a
# rollback, or an older tree deployed), and a dirty schema.

test_migrate_script_apply_skips_when_database_is_ahead() {
  fake_migrate "5"
  run_script apply
  assert_ok
  assert_contains "$OUT" 'newer than' "warns that the schema is ahead"
  assert_not_contains "$(cat "$WORK/calls")" ' up$' "doesn't run up (it would fail: no migration found for version 5)"
}

test_migrate_script_wait_tolerates_database_ahead() {
  fake_migrate "5"
  run_script wait
  assert_ok
  assert_contains "$OUT" 'newer than' "warns that the schema is ahead"
}

test_migrate_script_apply_refuses_dirty_database() {
  fake_migrate "3 (dirty)"
  run_script apply
  assert_render_fails 'dirty'
  assert_contains "$OUT" 'docs/runbooks/migrations.md' "points at the runbook"
  assert_not_contains "$(cat "$WORK/calls")" ' up$' "doesn't retry a failed migration on top of a dirty schema"
}

test_migrate_script_apply_failure_points_at_runbook() {
  fake_migrate "1"
  : >"$WORK/up-fails"
  run_script apply
  assert_render_fails 'docs/runbooks/migrations.md'
}

test_migrate_script_wait_keeps_waiting_while_behind() {
  fake_migrate "2"
  WAIT_MAX_ATTEMPTS=2 run_script wait
  assert_render_fails 'waiting'
  assert_not_contains "$(cat "$WORK/calls")" ' up$' "wait never migrates"
}
