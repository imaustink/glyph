# shellcheck shell=bash
# DI-03 — database durability: the CNPG Cluster, backups and their alerts.

BACKUP_ON=(
  --set cnpg.backup.enabled=true
  --set cnpg.backup.destinationPath=s3://bucket/glyph
  --set cnpg.backup.s3.secretName=glyph-backup-s3
)

test_cnpg_cluster_survives_helm_uninstall() {
  render
  assert_ok
  local c; c="$(doc Cluster glyph-db)"
  assert_contains "$c" 'helm.sh/resource-policy: keep' \
    "Cluster must carry resource-policy: keep so uninstall/prune never deletes the database"
}

test_cnpg_cluster_name_default_unchanged_for_existing_installs() {
  render
  assert_ok
  assert_contains "$(doc Cluster glyph-db)" 'kind: Cluster' "release glyph → Cluster glyph-db"
  helm_out_r() { set +e; OUT="$(helm template r "$CHART" 2>&1)"; STATUS=$?; set -e; }
  helm_out_r
  assert_contains "$(doc Cluster r-glyph-db)" 'kind: Cluster' "release r → Cluster r-glyph-db"
}

test_cnpg_cluster_name_can_be_pinned() {
  render --set cnpg.clusterName=pinned-db --set fullnameOverride=renamed "${BACKUP_ON[@]}"
  assert_ok
  assert_contains "$(doc Cluster pinned-db)" 'kind: Cluster' "cnpg.clusterName pins the Cluster name"
  assert_contains "$(doc Deployment renamed-api)" 'name: pinned-db-app' "API reads the pinned cluster's app secret"
  assert_contains "$(doc Deployment renamed-api)" 'pinned-db-rw' "API waits on the pinned cluster's rw service"
  assert_contains "$(doc ScheduledBackup pinned-db-backup)" 'name: pinned-db$' "ScheduledBackup targets the pinned cluster"
}

test_cnpg_backup_renders_credentials_from_secret() {
  render "${BACKUP_ON[@]}"
  assert_ok
  local c; c="$(doc Cluster glyph-db)"
  assert_contains "$c" 's3Credentials:' "s3Credentials rendered when secretName set"
  assert_contains "$c" 'name: glyph-backup-s3' "secret name used"
}

test_cnpg_backup_without_credentials_fails() {
  render --set cnpg.backup.enabled=true --set cnpg.backup.destinationPath=s3://bucket/glyph
  assert_render_fails 'cnpg.backup.s3.secretName'
}

test_cnpg_backup_with_iam_role_needs_no_secret() {
  render --set cnpg.backup.enabled=true --set cnpg.backup.destinationPath=s3://bucket/glyph \
    --set cnpg.backup.s3.inheritFromIAMRole=true
  assert_ok
  local c; c="$(doc Cluster glyph-db)"
  # CNPG puts inheritFromIAMRole under s3Credentials.
  assert_contains "$c" 'inheritFromIAMRole: true' "IAM role credentials rendered"
  assert_not_contains "$c" 'accessKeyId:' "no secret-based credentials"
}

test_cnpg_backup_without_destination_fails() {
  render --set cnpg.backup.enabled=true --set cnpg.backup.s3.secretName=glyph-backup-s3
  assert_render_fails 'cnpg.backup.destinationPath'
}

test_cnpg_scheduled_backup_default_is_six_field_cron() {
  render "${BACKUP_ON[@]}"
  assert_ok
  assert_contains "$(doc ScheduledBackup glyph-db-backup)" 'schedule: "0 0 0 \* \* \*"' \
    "CNPG schedules take 6 fields (seconds first)"
}

test_cnpg_scheduled_backup_five_field_cron_gets_seconds() {
  render "${BACKUP_ON[@]}" --set 'cnpg.backup.schedule=30 2 * * *'
  assert_ok
  assert_contains "$(doc ScheduledBackup glyph-db-backup)" 'schedule: "0 30 2 \* \* \*"' \
    "a crontab-style 5-field schedule is given a seconds field"
}

test_cnpg_backup_required_guard_fails_without_backups() {
  render --set cnpg.backup.required=true
  assert_render_fails 'cnpg.backup.required'
}

test_cnpg_backup_required_guard_passes_with_backups() {
  render --set cnpg.backup.required=true "${BACKUP_ON[@]}"
  assert_ok
}

test_cnpg_backups_stay_opt_in_by_default() {
  render
  assert_ok
  assert_not_contains "$OUT" '^kind: ScheduledBackup' "no ScheduledBackup by default"
  assert_not_contains "$(doc Cluster glyph-db)" 'barmanObjectStore' "no object store by default"
}

test_cnpg_prometheus_rule_alerts_on_archiver_and_backup_age() {
  render "${BACKUP_ON[@]}" --set cnpg.monitoring.enabled=true --set cnpg.monitoring.prometheusRule.enabled=true
  assert_ok
  local r; r="$(doc PrometheusRule glyph-db-alerts)"
  assert_contains "$r" 'kind: PrometheusRule' "PrometheusRule rendered"
  assert_contains "$r" 'cnpg_pg_stat_archiver_failed_count' "alerts on WAL archiving failures"
  assert_contains "$r" 'cnpg_pg_stat_archiver_seconds_since_last_archival' "alerts on stalled WAL archiving"
  assert_contains "$r" 'cnpg_collector_last_available_backup_timestamp' "alerts on backup age"
  assert_contains "$r" 'pod=~"glyph-db-\[0-9\]\+"' "scoped to this cluster's pods"
}

test_cnpg_prometheus_rule_off_by_default() {
  render
  assert_ok
  assert_not_contains "$OUT" '^kind: PrometheusRule' "no PrometheusRule by default"
}
