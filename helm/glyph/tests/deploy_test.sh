# shellcheck shell=bash
# DI-18 — scripts/deploy.sh, the manual deploy path. It must never ship
# anything but the tip of origin/main, must use the same tree-hash tags as CD
# (not :latest under pullPolicy IfNotPresent), and must deploy the way CD does
# (through Argo CD, scripts/argocd-deploy.sh), waiting for the rollout without
# rolling back on its own.

# _deploy_repo — a throwaway clone (with an origin) holding deploy.sh, plus
# stub docker/helm/kubectl/argocd/gh that log their arguments to $WORK/calls.
_deploy_repo() {
  local g=(git -c user.name=t -c user.email=t@example.com -c init.defaultBranch=main)
  "${g[@]}" init -q --bare "$WORK/origin.git"
  "${g[@]}" init -q "$WORK/repo"
  REPO="$WORK/repo"
  mkdir -p "$REPO/scripts" "$REPO/api/migrations" "$REPO/helm/glyph"
  cp "$REPO_ROOT/scripts/deploy.sh" "$REPO/scripts/deploy.sh"
  cp "$REPO_ROOT/scripts/argocd-deploy.sh" "$REPO/scripts/argocd-deploy.sh"
  : >"$REPO/api/migrations/000001_init.up.sql"
  echo "name: glyph" >"$REPO/helm/glyph/Chart.yaml"
  # The repo's real ignore files, so the fixture can't hide what the real
  # tree would show `git status`.
  cp "$REPO_ROOT/.gitignore" "$REPO/.gitignore"
  cp "$REPO_ROOT/.dockerignore" "$REPO/.dockerignore"
  "${g[@]}" -C "$REPO" add -A
  "${g[@]}" -C "$REPO" commit -q -m init
  # The operator's production values: an untracked local file, created after
  # the commit, where deploy.sh looks for it by default.
  echo "{}" >"$REPO/values-production.yaml"
  "${g[@]}" -C "$REPO" remote add origin "$WORK/origin.git"
  "${g[@]}" -C "$REPO" push -q origin HEAD:main 2>/dev/null
  "${g[@]}" -C "$REPO" branch -q -u origin/main 2>/dev/null || true
  GITC=("${g[@]}" -C "$REPO")

  mkdir -p "$WORK/bin"
  for tool in docker helm kubectl argocd gh; do
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
  # argocd: an Argo CD Application whose sync of whatever commit it was last
  # pointed at succeeds at once.
  cat >"$WORK/bin/argocd" <<EOF
#!/bin/sh
echo "argocd \$*" >>"$WORK/calls"
case "\$*" in
  "app get glyph -o json")
    state=\$(cat "$WORK/op-state" 2>/dev/null || echo "Succeeded|old|t0|")
    IFS='|' read -r phase rev started msg <<STATE
\$state
STATE
    printf '{"spec":{"sources":[{"repoURL":"https://github.com/imaustink/glyph.git"}]},"status":{"operationState":{"phase":"%s","startedAt":"%s","syncResult":{"revisions":["%s"]}}}}\n' "\$phase" "\$started" "\$rev" ;;
  "app set glyph "*)
    sha=\$(printf '%s' "\$*" | sed -n 's/.*--revision \([0-9a-f]*\).*/\1/p')
    echo "Succeeded|\$sha|t1|" >"$WORK/op-state" ;;
esac
exit 0
EOF
  chmod +x "$WORK/bin/argocd"
  : >"$WORK/calls"
}

_run_deploy() {
  set +e
  OUT="$(cd "$REPO" && PATH="$WORK/bin:$PATH" POLL_SECONDS=0 bash scripts/deploy.sh "$@" 2>&1)"
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
  assert_not_contains "$CALLS" '^(docker (build|push)|helm|kubectl|argocd)' "nothing built or deployed"
}

test_deploy_refuses_dirty_tree() {
  _deploy_repo
  echo dirty >>"$REPO/helm/glyph/Chart.yaml"
  _run_deploy
  assert_render_fails 'uncommitted'
  assert_not_contains "$CALLS" '^(docker (build|push)|helm|kubectl|argocd)' "nothing built or deployed"
}

# Review (PR #46): an operator may still keep an untracked
# values-production.yaml in the repo root (gitignored); it must not trip the
# dirty-tree guard.
test_deploy_local_values_file_does_not_trip_dirty_guard() {
  _deploy_repo
  _run_deploy
  assert_ok
  assert_not_contains "$OUT" 'uncommitted' "values-production.yaml is not a local change"
}

# The guard stays strict for other untracked files: the frontend image is
# built from `COPY . .` and collab from `COPY src/lib` / `COPY collab`, so an
# untracked file can change an image whose tag (the tree hash) says it didn't.
test_deploy_refuses_untracked_file() {
  _deploy_repo
  mkdir -p "$REPO/src/lib"
  echo "export const x = 1" >"$REPO/src/lib/untracked.ts"
  _run_deploy
  assert_render_fails 'uncommitted'
  assert_not_contains "$CALLS" '^(docker (build|push)|helm|kubectl|argocd)' "nothing built or deployed"
}

# values-production.yaml holds production secrets; with `COPY . .` it would
# otherwise be sent in the frontend build context and land in the builder
# stage.
test_dockerignore_excludes_values_production() {
  local f; f="$(cat "$REPO_ROOT/.dockerignore")"
  assert_contains "$f" '^/?values-production\.yaml$' ".dockerignore keeps the production values out of build contexts"
}

test_deploy_refuses_while_cd_is_running() {
  _deploy_repo
  echo "in_progress" >"$WORK/gh-runs"
  _run_deploy
  assert_render_fails 'CD'
  assert_not_contains "$CALLS" '^(helm|kubectl|argocd)' "nothing deployed"
}

test_deploy_uses_tree_hash_tags_and_waits() {
  _deploy_repo
  local tree; tree="$("${GITC[@]}" rev-parse 'HEAD^{tree}')"
  _run_deploy
  assert_ok
  assert_not_contains "$CALLS" ':latest' "never :latest"
  assert_contains "$CALLS" "docker push docker.io/blackmarket/glyph-api:$tree" "api pushed under the tree hash"
  assert_contains "$CALLS" "docker push docker.io/blackmarket/glyph-collab:$tree" "collab pushed under the tree hash"
  local head; head="$("${GITC[@]}" rev-parse HEAD)"
  assert_contains "$CALLS" "^argocd app set glyph --source-position 1 --revision $head" "points the Argo CD app at the commit"
  assert_contains "$CALLS" "--helm-set api.image.tag=$tree" "deploys the tree-hash tag"
  assert_contains "$CALLS" "^argocd app wait glyph --health" "waits for the rollout"
  assert_not_contains "$CALLS" '^kubectl' "no Kubernetes access"
  assert_not_contains "$CALLS" '^helm' "never helm-upgrades a release Argo CD owns"
  assert_not_contains "$CALLS" 'rollout restart' "no extra restart"
}

# Review (PR #46): same policy as CD, no automatic rollback (see cd_test.sh).
test_deploy_upgrade_does_not_roll_back_automatically() {
  _deploy_repo
  _run_deploy
  assert_ok
  assert_contains "$CALLS" 'app set glyph' "deploy ran"
  assert_not_contains "$CALLS" '^helm' "no helm upgrade/rollback"
  assert_not_contains "$CALLS" 'rollback' "no scripted rollback"
  assert_contains "$CALLS" 'app wait glyph .*--timeout' "bounded wait"
}
