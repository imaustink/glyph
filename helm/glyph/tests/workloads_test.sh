# shellcheck shell=bash
# Pod lifecycle: graceful shutdown and disruption budgets.

COLLAB_ON=(--set collab.enabled=true --set collab.serviceToken=t)

# DI-15 — collab drains parked documents on SIGTERM; give it time, and stop
# new traffic first.
test_collab_has_long_grace_period_and_prestop() {
  render "${COLLAB_ON[@]}"
  assert_ok
  local d; d="$(doc Deployment glyph-collab)"
  assert_contains "$d" 'terminationGracePeriodSeconds: 60$' "collab grace period is 60s"
  assert_contains "$d" 'preStop:' "collab has a preStop hook"
  assert_contains "$d" '"sleep", "5"' "preStop sleeps so endpoints drop the pod before SIGTERM"
  assert_contains "$d" 'readinessProbe:' "collab keeps its readiness probe"
}

test_collab_grace_period_is_configurable() {
  render "${COLLAB_ON[@]}" --set collab.terminationGracePeriodSeconds=120 --set collab.preStopSleepSeconds=0
  assert_ok
  local d; d="$(doc Deployment glyph-collab)"
  assert_contains "$d" 'terminationGracePeriodSeconds: 120$' "grace period from values"
  assert_not_contains "$d" 'preStop:' "preStopSleepSeconds=0 disables the hook"
}
