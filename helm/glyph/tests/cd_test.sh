# shellcheck shell=bash
# DI-17 — the CD deploy step (.github/workflows/cd.yml).

_cd_upgrade_step() {
  awk '/- name: Helm upgrade/{on=1} on&&/- name:/&&!/Helm upgrade/{exit} on' \
    "$REPO_ROOT/.github/workflows/cd.yml"
}

test_cd_helm_upgrade_is_atomic_and_waits() {
  local s; s="$(_cd_upgrade_step)"
  assert_contains "$s" 'helm upgrade glyph' "Helm upgrade step found"
  assert_contains "$s" '--atomic' "a failed upgrade (e.g. a failed migration hook) rolls back"
  assert_contains "$s" '--wait' "waits for every Deployment, collab included"
}

test_cd_does_not_restart_rollouts() {
  local f; f="$(cat "$REPO_ROOT/.github/workflows/cd.yml")"
  assert_not_contains "$f" 'rollout restart' "tree-hash tags already roll the pods; a restart rolls the API twice"
}

test_cd_pins_helm_v3() {
  local f; f="$(cat "$REPO_ROOT/.github/workflows/cd.yml")"
  assert_contains "$f" 'version: v3\.' "Helm is pinned so --atomic keeps its meaning"
}
