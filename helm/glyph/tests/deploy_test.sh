# shellcheck shell=bash
# DI-18 — scripts/deploy.sh, the manual deploy path. It must never ship
# anything but the tip of origin/main, must use the same tree-hash tags as CD
# (not :latest under pullPolicy IfNotPresent), and must upgrade atomically.

# _deploy_repo — a throwaway clone (with an origin) holding deploy.sh, plus
# stub docker/helm/kubectl/gh that log their arguments to $WORK/calls.
_deploy_repo() {
  local g=(git -c user.name=t -c user.email=t@example.com -c init.defaultBranch=main)
  "${g[@]}" init -q --bare "$WORK/origin.git"
  "${g[@]}" init -q "$WORK/repo"
  REPO="$WORK/repo"
  mkdir -p "$REPO/scripts" "$REPO/api/migrations" "$REPO/helm/glyph"
  cp "$REPO_ROOT/scripts/deploy.sh" "$REPO/scripts/deploy.sh"
  : >"$REPO/api/migrations/000001_init.up.sql"
  echo "name: glyph" >"$REPO/helm/glyph/Chart.yaml"
  printf 'helm/glyph/migrations/\nvalues-production.yaml\n' >"$REPO/.gitignore"
  echo "{}" >"$REPO/values-production.yaml"
  "${g[@]}" -C "$REPO" add -A
  "${g[@]}" -C "$REPO" commit -q -m init
  "${g[@]}" -C "$REPO" remote add origin "$WORK/origin.git"
  "${g[@]}" -C "$REPO" push -q origin HEAD:main 2>/dev/null
  "${g[@]}" -C "$REPO" branch -q -u origin/main 2>/dev/null || true
  GITC=("${g[@]}" -C "$REPO")

  mkdir -p "$WORK/bin"
  for tool in docker helm kubectl gh; do
    cat >"$WORK/bin/$tool" <<EOF
#!/bin/sh
echo "$tool \$*" >>"$WORK/calls"
case "$tool \$1 \$2" in
  "docker manifest inspect") exit 1 ;;  # nothing in the registry yet
  "gh run list") cat "$WORK/gh-runs" 2>/dev/null ;;
esac
exit 0
EOF
    chmod +x "$WORK/bin/$tool"
  done
  : >"$WORK/calls"
}

_run_deploy() {
  set +e
  OUT="$(cd "$REPO" && PATH="$WORK/bin:$PATH" bash scripts/deploy.sh "$@" 2>&1)"
  STATUS=$?
  set -e
  CALLS="$(cat "$WORK/calls")"
}

test_deploy_refuses_unless_at_origin_main_tip() {
  _deploy_repo
  echo change >"$REPO/local.txt"
  "${GITC[@]}" add -A
  "${GITC[@]}" commit -q -m "local only"
  _run_deploy
  assert_render_fails 'origin/main'
  assert_not_contains "$CALLS" '^(docker (build|push)|helm)' "nothing built or deployed"
}

test_deploy_refuses_dirty_tree() {
  _deploy_repo
  echo dirty >>"$REPO/helm/glyph/Chart.yaml"
  _run_deploy
  assert_render_fails 'uncommitted'
  assert_not_contains "$CALLS" '^(docker (build|push)|helm)' "nothing built or deployed"
}

test_deploy_refuses_while_cd_is_running() {
  _deploy_repo
  echo "in_progress" >"$WORK/gh-runs"
  _run_deploy
  assert_render_fails 'CD'
  assert_not_contains "$CALLS" '^helm' "nothing deployed"
}

test_deploy_uses_tree_hash_tags_and_atomic_upgrade() {
  _deploy_repo
  local tree; tree="$("${GITC[@]}" rev-parse 'HEAD^{tree}')"
  _run_deploy
  assert_ok
  assert_not_contains "$CALLS" ':latest' "never :latest"
  assert_contains "$CALLS" "docker push docker.io/blackmarket/glyph-api:$tree" "api pushed under the tree hash"
  assert_contains "$CALLS" "docker push docker.io/blackmarket/glyph-collab:$tree" "collab pushed under the tree hash"
  assert_contains "$CALLS" "^helm upgrade glyph helm/glyph .*--atomic" "atomic upgrade"
  assert_contains "$CALLS" "^helm upgrade .*--wait" "waits for the rollout"
  assert_contains "$CALLS" "^helm upgrade .*api.image.tag=$tree" "deploys the tree-hash tag"
  assert_not_contains "$CALLS" 'rollout restart' "no extra restart"
}

# Review (PR #46): same policy as CD, no automatic rollback (see cd_test.sh).
test_deploy_upgrade_does_not_roll_back_automatically() {
  _deploy_repo
  _run_deploy
  assert_ok
  assert_contains "$CALLS" '^helm upgrade glyph helm/glyph ' "upgrade ran"
  assert_not_contains "$CALLS" '--atomic' "no automatic rollback"
  assert_not_contains "$CALLS" '--rollback-on-failure' "nor its Helm 4 name"
  assert_not_contains "$CALLS" '^helm rollback' "no scripted rollback"
  assert_contains "$CALLS" '^helm upgrade .*--wait' "waits for the rollout"
  assert_contains "$CALLS" '^helm upgrade .*--timeout' "bounded wait"
}
