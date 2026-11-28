# shellcheck shell=bash
# scripts/argocd-deploy.sh against a fake `argocd` CLI / Application.
#
# _fake_argocd BEFORE AFTER_SET [AFTER_SYNC]
#   Each argument is the app's "phase|revision|startedAt|message" operation
#   state: before `app set`, after it, and after the script explicitly
#   requests a sync. Every argocd call is logged to $WORK/calls, and the
#   environment it saw to $WORK/env. Touch $WORK/wait-fails to make
#   `app wait` fail; put a URL in $WORK/repo to change sources[0].repoURL.
_fake_argocd() {
  mkdir -p "$WORK/bin"
  printf '%s\n' "$1" >"$WORK/state"
  printf '%s\n' "$2" >"$WORK/state-after-set"
  printf '%s\n' "${3:-$2}" >"$WORK/state-after-sync"
  cat >"$WORK/bin/argocd" <<EOF
#!/bin/sh
echo "argocd \$*" >>"$WORK/calls"
echo "server=\${ARGOCD_SERVER:-} opts=\${ARGOCD_OPTS:-} token=\${ARGOCD_AUTH_TOKEN:+set}" >"$WORK/env"
case "\$*" in
  "app get glyph -o json")
    repo=\$(cat "$WORK/repo" 2>/dev/null || echo https://github.com/imaustink/glyph.git)
    IFS='|' read -r phase rev started message <"$WORK/state"
    printf '{"spec":{"sources":[{"repoURL":"%s"}]},"status":{"operationState":{"phase":"%s","startedAt":"%s","message":"%s","syncResult":{"revisions":["%s"]}}}}\n' \
      "\$repo" "\$phase" "\$started" "\$message" "\$rev" ;;
  "app set glyph "*) cp "$WORK/state-after-set" "$WORK/state" ;;
  "app sync glyph "*) cp "$WORK/state-after-sync" "$WORK/state" ;;
  "app wait glyph "*) [ -f "$WORK/wait-fails" ] && exit 1 ;;
esac
exit 0
EOF
  chmod +x "$WORK/bin/argocd"
  : >"$WORK/calls"
}

_run_argocd_deploy() {
  set +e
  OUT="$(PATH="$WORK/bin:$PATH" POLL_SECONDS=0 TRIGGER_AFTER_SECONDS="${TRIGGER_AFTER_SECONDS:-0}" \
    DEPLOY_TIMEOUT_SECONDS="${DEPLOY_TIMEOUT_SECONDS:-5}" ARGOCD_AUTH_TOKEN="${TOKEN-tok}" \
    bash "$REPO_ROOT/scripts/argocd-deploy.sh" abc123 tree456 2>&1)"
  STATUS=$?
  set -e
  CALLS="$(cat "$WORK/calls")"
}

test_argocd_deploy_sets_chart_revision_and_images_together() {
  _fake_argocd "Succeeded|old|t0|" "Succeeded|abc123|t1|"
  _run_argocd_deploy
  assert_ok
  local set; set="$(grep '^argocd app set' <<<"$CALLS")"
  assert_eq "$(grep -c '^argocd app set' <<<"$CALLS")" "1" "a single update"
  assert_contains "$set" '--source-position 1 --revision abc123' "chart at the commit"
  for img in frontend api collab; do
    assert_contains "$set" "--helm-set $img.image.tag=tree456" "$img at the tree hash"
    assert_contains "$set" "--helm-set $img.image.repository=docker.io/blackmarket/glyph-$img" "$img repository pinned"
  done
  assert_contains "$CALLS" '^argocd app wait glyph --health --timeout' "waits for the app to be healthy"
  assert_not_contains "$CALLS" 'app sync' "auto-sync started it; no explicit sync needed"
  assert_not_contains "$CALLS" 'kubectl' "no Kubernetes access"
}

test_argocd_deploy_uses_the_in_cluster_server_with_a_token() {
  _fake_argocd "Succeeded|old|t0|" "Succeeded|abc123|t1|"
  _run_argocd_deploy
  assert_ok
  assert_eq "$(cat "$WORK/env")" "server=argocd-server.argocd.svc.cluster.local:80 opts=--plaintext --grpc-web token=set" "in-cluster API, token passed through"
}

# scripts/deploy.sh run by a person: no token, so the CLI's own login decides
# the server; the script must not point it elsewhere.
test_argocd_deploy_without_a_token_uses_the_cli_login() {
  _fake_argocd "Succeeded|old|t0|" "Succeeded|abc123|t1|"
  TOKEN="" _run_argocd_deploy
  assert_ok
  assert_eq "$(cat "$WORK/env")" "server= opts= token=" "no server override"
}

test_argocd_deploy_refuses_a_reordered_source() {
  _fake_argocd "Succeeded|old|t0|" "Succeeded|abc123|t1|"
  echo https://example.com/other.git >"$WORK/repo"
  _run_argocd_deploy
  assert_render_fails "first source is 'https://example.com/other.git'"
  assert_not_contains "$CALLS" 'app set' "nothing changed"
}

test_argocd_deploy_fails_when_the_sync_fails() {
  _fake_argocd "Succeeded|old|t0|" "Failed|abc123|t1|job glyph-migrate failed"
  _run_argocd_deploy
  assert_render_fails 'sync of abc123 ended Failed: job glyph-migrate failed'
  assert_not_contains "$CALLS" 'app wait' "doesn't wait on a rollout that won't happen"
  assert_not_contains "$CALLS" 'rollback' "and doesn't roll back"
}

# A re-run for a commit whose sync already failed: that old operation must
# not count, and since Argo won't retry it on its own, the script asks.
test_argocd_deploy_retriggers_a_previously_failed_commit() {
  _fake_argocd "Failed|abc123|t0|boom" "Failed|abc123|t0|boom" "Succeeded|abc123|t1|"
  _run_argocd_deploy
  assert_ok
  assert_contains "$CALLS" '^argocd app sync glyph --async' "explicit sync requested"
  assert_contains "$CALLS" 'app wait glyph --health' "then waits for health"
}

test_argocd_deploy_times_out() {
  _fake_argocd "Succeeded|old|t0|" "Running|abc123|t1|waiting for hook"
  DEPLOY_TIMEOUT_SECONDS=1 _run_argocd_deploy
  assert_render_fails 'timed out'
}

test_argocd_deploy_fails_when_the_app_never_gets_healthy() {
  _fake_argocd "Succeeded|old|t0|" "Succeeded|abc123|t1|"
  touch "$WORK/wait-fails"
  _run_argocd_deploy
  if [[ "$STATUS" -eq 0 ]]; then _fail "expected failure when the app doesn't become healthy"; fi
}
