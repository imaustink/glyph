# shellcheck shell=bash
# argocd.enabled — the chart as Argo CD renders it (`helm template`, so never
# an upgrade): migrations run as a Sync hook between the database and the
# Deployments, ordered with sync waves.

# Argo CD mode with the image tags CD / the ApplicationSet always supply.
ARGO=(--set argocd.enabled=true --set frontend.image.tag=t1 --set api.image.tag=t1 --set collab.image.tag=t1)

test_argocd_requires_immutable_image_tags() {
  render --set argocd.enabled=true --set api.image.tag=t1
  assert_render_fails 'frontend.image.tag must be an immutable tag under Argo CD'
  render --set argocd.enabled=true --set frontend.image.tag=latest --set api.image.tag=t1
  assert_render_fails 'frontend.image.tag must be an immutable tag'
  render --set argocd.enabled=true --set frontend.image.tag=t1 --set api.image.tag=t1 \
    --set collab.enabled=true --set collab.serviceToken=x
  assert_render_fails 'collab.image.tag must be an immutable tag'
  render --set argocd.enabled=true --set frontend.image.tag=t1 --set api.image.tag=t1
  assert_ok
  render
  assert_ok
}

test_argocd_migrate_job_is_sync_hook_between_db_and_deployments() {
  render "${ARGO[@]}"
  assert_ok
  local j; j="$(doc Job glyph-migrate)"
  assert_contains "$j" 'kind: Job' "stable name, no Helm revision suffix (always 1 under Argo)"
  assert_contains "$j" 'argocd.argoproj.io/hook: Sync' "re-runs on every sync even though its spec never changes"
  assert_contains "$j" 'argocd.argoproj.io/sync-wave: "-1"' "after the database, before the Deployments"
  assert_contains "$j" 'argocd.argoproj.io/hook-delete-policy: BeforeHookCreation' "replaced by the next sync's Job"
  assert_not_contains "$j" 'helm.sh/hook' "no Helm hook"
  assert_contains "$j" 'name: glyph-migrations$' "reads the release ConfigMap"
  assert_contains "$(doc ConfigMap glyph-migrations)" 'argocd.argoproj.io/sync-wave: "-2"' "migrations applied before the Job"
  assert_contains "$(doc Cluster glyph-db)" 'argocd.argoproj.io/sync-wave: "-2"' "database created before the Job waits on it"
  assert_not_contains "$OUT" 'name: glyph-migrations-hook' "no Helm hook ConfigMap"
}

# Argo renders with `helm template`, but someone may still render it as an
# upgrade; argocd.enabled must win.
test_argocd_mode_ignores_is_upgrade() {
  render --is-upgrade "${ARGO[@]}"
  assert_ok
  assert_not_contains "$OUT" 'helm.sh/hook' "no Helm hooks"
  assert_contains "$(doc Job glyph-migrate)" 'argocd.argoproj.io/hook: Sync' "still the Argo hook"
}

test_argocd_deployments_stay_in_default_wave() {
  render "${ARGO[@]}" --set collab.enabled=true --set collab.serviceToken=x
  assert_ok
  for d in glyph-api glyph-frontend glyph-collab; do
    assert_not_contains "$(doc Deployment "$d")" 'sync-wave' "$d in wave 0, after the migrate hook"
  done
}

test_cnpg_retained_on_delete_by_default() {
  render "${ARGO[@]}"
  assert_ok
  local c; c="$(doc Cluster glyph-db)"
  assert_contains "$c" 'helm.sh/resource-policy: keep' "kept by helm uninstall"
  assert_contains "$c" 'argocd.argoproj.io/sync-options: Delete=false' "kept by Argo prune / app deletion"
}

test_cnpg_disposable_when_retain_off() {
  render "${ARGO[@]}" --set cnpg.retainOnDelete=false
  assert_ok
  local c; c="$(doc Cluster glyph-db)"
  assert_not_contains "$c" 'resource-policy' "previews delete their database with the app"
  assert_not_contains "$c" 'Delete=false' "previews delete their database with the app"
  render --set cnpg.retainOnDelete=false
  assert_ok
  assert_not_contains "$(doc Cluster glyph-db)" 'annotations:' "no empty annotations block"
}

test_oidc_state_prefix_env() {
  render --set api.oidc.enabled=true --set api.oidc.clientSecret=s --set api.sessionSecret=00 \
    --set api.oidc.statePrefix=pr-7.
  assert_ok
  local d; d="$(doc Deployment glyph-api)"
  assert_contains "$d" 'name: OIDC_STATE_PREFIX' "prefix passed to the API"
  assert_contains "$d" 'value: "pr-7\."' "with its value"
  render --set api.oidc.enabled=true --set api.oidc.clientSecret=s --set api.sessionSecret=00
  assert_not_contains "$(doc Deployment glyph-api)" 'OIDC_STATE_PREFIX' "unset by default"
}

test_seed_job_off_by_default() {
  render "${ARGO[@]}"
  assert_ok
  assert_not_contains "$OUT" 'seed-demo' "no demo data unless asked for"
}

test_seed_job_is_postsync_hook_under_argocd() {
  render "${ARGO[@]}" --set seed.enabled=true
  assert_ok
  local j; j="$(doc Job glyph-seed)"
  assert_contains "$j" 'argocd.argoproj.io/hook: PostSync' "after migrations and a healthy rollout"
  assert_contains "$j" 'args: \["seed-demo"\]' "runs the seed subcommand"
  assert_contains "$j" 'name: GLYPH_SEED_DEMO' "with the explicit opt-in"
  assert_contains "$j" 'image: "docker.io/blackmarket/glyph-api:t1"' "from this release's API image"
  render --set seed.enabled=true
  assert_ok
  assert_contains "$(doc Job glyph-seed)" '"helm.sh/hook": post-install,post-upgrade' "Helm hook without Argo"
}
