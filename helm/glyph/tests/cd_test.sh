# shellcheck shell=bash
# DI-17 — the CD deploy step (.github/workflows/cd.yml). Production is an
# Argo CD Application; CD points it at the merged commit through
# scripts/argocd-deploy.sh (tested in argocd_deploy_test.sh).

_cd_deploy_job() {
  awk '/^  deploy:/{on=1} on' "$REPO_ROOT/.github/workflows/cd.yml" | grep -vE '^[[:space:]]*#'
}

test_cd_does_not_restart_rollouts() {
  local f; f="$(cat "$REPO_ROOT/.github/workflows/cd.yml")"
  assert_not_contains "$f" 'rollout restart' "tree-hash tags already roll the pods; a restart rolls the API twice"
}

test_cd_pins_kubectl() {
  local j; j="$(_cd_deploy_job)"
  assert_contains "$j" 'setup-kubectl' "kubectl installed"
  assert_contains "$j" 'version: v1\.' "and pinned"
}

test_cd_deploys_commit_and_tree_hash_through_argocd() {
  local j; j="$(_cd_deploy_job)"
  assert_contains "$j" 'scripts/argocd-deploy\.sh "\$GITHUB_SHA" "\$\(git rev-parse .HEAD\^\{tree\}.\)"' \
    "chart at this commit, images at its tree hash"
  assert_not_contains "$j" 'helm upgrade' "Argo CD owns the release now"
}

# Review (PR #46): an automatic rollback after a failed release crash-loops
# the API (see argocd-deploy.sh). Neither CD nor the script may roll back.
test_cd_does_not_roll_back_automatically() {
  local f; f="$(grep -vE '^[[:space:]]*#' "$REPO_ROOT/.github/workflows/cd.yml" "$REPO_ROOT/scripts/argocd-deploy.sh")"
  assert_not_contains "$f" 'rollback' "no scripted rollback"
  assert_not_contains "$f" '--atomic' "no automatic rollback"
}

# Migrations reach the chart through the helm/glyph/migrations symlink, so
# Argo CD renders them straight from git; nothing may copy over it.
test_chart_migrations_are_a_symlink_to_api_migrations() {
  local l; l="$(readlink "$REPO_ROOT/helm/glyph/migrations" || true)"
  assert_eq "$l" "../../api/migrations" "helm/glyph/migrations -> ../../api/migrations"
  local f; f="$(cat "$REPO_ROOT/.github/workflows/cd.yml" "$REPO_ROOT/scripts/deploy.sh")"
  assert_not_contains "$f" 'cp -r api/migrations' "no deploy-time copy that would replace the symlink"
}
