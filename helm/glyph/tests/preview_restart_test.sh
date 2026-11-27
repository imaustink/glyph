# shellcheck shell=bash
# scripts/preview-restart-stuck-pods.sh against a fake kubectl.
#
# _fake_pods LISTING — every `get pods` prints LISTING (the script's
# jsonpath output: "name reasons..." per line). Calls go to $WORK/calls.
_fake_pods() {
  mkdir -p "$WORK/bin"
  printf '%s' "$1" >"$WORK/pods"
  cat >"$WORK/bin/kubectl" <<EOF
#!/bin/sh
echo "kubectl \$*" >>"$WORK/calls"
case "\$*" in
  *"get pods"*) [ -f "$WORK/get-fails" ] && { echo forbidden >&2; exit 1; }; cat "$WORK/pods" ;;
esac
exit 0
EOF
  chmod +x "$WORK/bin/kubectl"
  : >"$WORK/calls"
}

_run_restart() {
  set +e
  OUT="$(PATH="$WORK/bin:$PATH" ROUNDS="${ROUNDS:-1}" ROUND_SECONDS=0 \
    bash "$REPO_ROOT/scripts/preview-restart-stuck-pods.sh" "$@" 2>&1)"
  STATUS=$?
  set -e
  CALLS="$(cat "$WORK/calls")"
}

test_preview_restart_deletes_only_pods_stuck_pulling() {
  _fake_pods "glyph-pr-7-api-a  ImagePullBackOff
glyph-pr-7-collab-b ErrImagePull
glyph-pr-7-frontend-c
glyph-pr-7-migrate-d ImagePullBackOff
glyph-pr-7-seed-e  ContainerCreating
"
  _run_restart 7
  assert_ok
  assert_contains "$CALLS" '-n glyph-preview get pods -l app.kubernetes.io/name=glyph,app.kubernetes.io/instance=glyph-pr-7' "only this PR's Glyph pods"
  assert_contains "$CALLS" 'delete pod --wait=false glyph-pr-7-api-a glyph-pr-7-collab-b glyph-pr-7-migrate-d' "containers and init containers stuck pulling"
  assert_not_contains "$CALLS" 'frontend-c' "a running pod is left alone"
  assert_not_contains "$CALLS" 'seed-e' "a pod still creating is left alone"
}

test_preview_restart_does_nothing_without_stuck_pods() {
  _fake_pods ""
  ROUNDS=2 _run_restart 7
  assert_ok
  assert_eq "$(grep -c 'get pods' <<<"$CALLS")" "2" "looks each round"
  assert_not_contains "$CALLS" 'delete' "nothing to restart"
  assert_contains "$OUT" 'pr-7: restarted 0 pod\(s\)' "says so"
}

test_preview_restart_fails_when_it_cannot_list_pods() {
  _fake_pods ""
  touch "$WORK/get-fails"
  _run_restart 7
  assert_render_fails 'forbidden'
}

test_preview_restart_rejects_a_non_number() {
  _fake_pods ""
  _run_restart '7,app=x'
  assert_render_fails 'not a PR number'
  assert_eq "$CALLS" "" "never calls kubectl"
}
