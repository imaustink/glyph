# shellcheck shell=bash
# DI-17 — the CD deploy step (.github/workflows/cd.yml).

_cd_upgrade_step() {
  awk '/- name: Helm upgrade/{on=1} on&&/- name:/&&!/Helm upgrade/{exit} on' \
    "$REPO_ROOT/.github/workflows/cd.yml"
}

test_cd_does_not_restart_rollouts() {
  local f; f="$(cat "$REPO_ROOT/.github/workflows/cd.yml")"
  assert_not_contains "$f" 'rollout restart' "tree-hash tags already roll the pods; a restart rolls the API twice"
}

test_cd_pins_helm_v3() {
  local f; f="$(cat "$REPO_ROOT/.github/workflows/cd.yml")"
  assert_contains "$f" 'version: v3\.' "Helm is pinned so the upgrade flags keep their meaning"
}

# Review (PR #46): an automatic rollback after a failed release crash-loops
# the API. The pre-upgrade hook has already applied this tree's migrations; a
# rollback to any pre-PR-#46 revision recreates API pods whose init container
# runs `migrate up` with older files and fails ("no migration found for
# version N"). So CD must not roll back on its own. A failed rollout leaves
# the old ReplicaSet serving, which is the safe state.
test_cd_helm_upgrade_does_not_roll_back_automatically() {
  local s; s="$(_cd_upgrade_step | grep -vE '^[[:space:]]*#')"
  assert_contains "$s" 'helm upgrade glyph' "Helm upgrade step found"
  assert_not_contains "$s" '--atomic' "no automatic rollback to a revision that may not tolerate the new schema"
  assert_not_contains "$s" '--rollback-on-failure' "nor its Helm 4 name"
  assert_contains "$s" '--wait' "still waits for every Deployment, so a bad rollout fails the job"
  assert_contains "$s" '--timeout' "and gives up after a bound"
  local f; f="$(grep -vE '^[[:space:]]*#' "$REPO_ROOT/.github/workflows/cd.yml")"
  assert_not_contains "$f" 'helm rollback' "no scripted rollback step either"
}
