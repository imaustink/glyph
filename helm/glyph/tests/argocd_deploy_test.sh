# shellcheck shell=bash
# scripts/argocd-deploy.sh against a fake kubectl / Argo CD Application.
#
# _fake_argocd BEFORE AFTER_PATCH [AFTER_TRIGGER]
#   Each argument is the app's "phase|revision|startedAt|message" operation
#   state: before the spec patch, after it, and after the script explicitly
#   requests a sync. Every kubectl call is logged to $WORK/calls.
_fake_argocd() {
  mkdir -p "$WORK/bin"
  printf '%s\n' "$1" >"$WORK/state"
  printf '%s\n' "$2" >"$WORK/state-after-patch"
  printf '%s\n' "${3:-$2}" >"$WORK/state-after-trigger"
  cat >"$WORK/bin/kubectl" <<EOF
#!/bin/sh
echo "kubectl \$*" >>"$WORK/calls"
case "\$*" in
  *"get application"*) cat "$WORK/state" ;;
  *"--type json"*) cp "$WORK/state-after-patch" "$WORK/state" ;;
  *'"operation"'*) cp "$WORK/state-after-trigger" "$WORK/state" ;;
  *"rollout status"*) [ -f "$WORK/rollout-fails" ] && exit 1 ;;
esac
exit 0
EOF
  chmod +x "$WORK/bin/kubectl"
  : >"$WORK/calls"
}

_run_argocd_deploy() {
  set +e
  OUT="$(PATH="$WORK/bin:$PATH" POLL_SECONDS=0 TRIGGER_AFTER_SECONDS="${TRIGGER_AFTER_SECONDS:-0}" \
    DEPLOY_TIMEOUT_SECONDS="${DEPLOY_TIMEOUT_SECONDS:-5}" \
    bash "$REPO_ROOT/scripts/argocd-deploy.sh" abc123 tree456 2>&1)"
  STATUS=$?
  set -e
  CALLS="$(cat "$WORK/calls")"
}

test_argocd_deploy_patches_chart_revision_and_images_together() {
  _fake_argocd "Succeeded|old|t0|" "Succeeded|abc123|t1|"
  _run_argocd_deploy
  assert_ok
  # The patch body spans lines of the log; nothing else here mentions these.
  local patch="$CALLS"
  assert_eq "$(grep -c -- '--type json' <<<"$CALLS")" "1" "a single patch"
  assert_contains "$patch" '"op": "test", "path": "/spec/sources/0/repoURL", "value": "https://github.com/imaustink/glyph.git"' "refuses to retarget a reordered source"
  assert_contains "$patch" '"/spec/sources/0/targetRevision", "value": "abc123"' "chart at the commit"
  for img in frontend api collab; do
    assert_contains "$patch" "\"$img.image.tag\", \"value\": \"tree456\"" "$img at the tree hash"
    assert_contains "$patch" "\"$img.image.repository\", \"value\": \"docker.io/blackmarket/glyph-$img\"" "$img repository pinned"
  done
  assert_contains "$CALLS" 'rollout status deployment -l app.kubernetes.io/instance=glyph --timeout' "waits for every Deployment"
  assert_not_contains "$CALLS" '"operation"' "auto-sync started it; no explicit sync needed"
}

test_argocd_deploy_fails_when_the_sync_fails() {
  _fake_argocd "Succeeded|old|t0|" "Failed|abc123|t1|job glyph-migrate failed"
  _run_argocd_deploy
  assert_render_fails 'sync of abc123 ended Failed: job glyph-migrate failed'
  assert_not_contains "$CALLS" 'rollout status' "doesn't wait on a rollout that won't happen"
  assert_not_contains "$CALLS" 'rollback' "and doesn't roll back"
}

# A re-run for a commit whose sync already failed: that old operation must
# not count, and since Argo won't retry it on its own, the script asks.
test_argocd_deploy_retriggers_a_previously_failed_commit() {
  _fake_argocd "Failed|abc123|t0|boom" "Failed|abc123|t0|boom" "Succeeded|abc123|t1|"
  _run_argocd_deploy
  assert_ok
  assert_contains "$CALLS" '"operation":\{"initiatedBy":\{"username":"glyph-cd"\},"sync"' "explicit sync requested"
  assert_contains "$CALLS" 'rollout status' "then waits for the rollout"
}

test_argocd_deploy_times_out() {
  _fake_argocd "Succeeded|old|t0|" "Running|abc123|t1|waiting for hook"
  DEPLOY_TIMEOUT_SECONDS=1 _run_argocd_deploy
  assert_render_fails 'timed out'
}

test_argocd_deploy_fails_when_rollout_fails() {
  _fake_argocd "Succeeded|old|t0|" "Succeeded|abc123|t1|"
  touch "$WORK/rollout-fails"
  _run_argocd_deploy
  if [[ "$STATUS" -eq 0 ]]; then _fail "expected failure when the rollout doesn't finish"; fi
}
