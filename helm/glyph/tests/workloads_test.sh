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

# DI-14 — turning collab off must not strand the draining collab pods: their
# final snapshots authenticate with the service token, so the API keeps it
# (and the chart keeps its Secret) while a token is still configured.
test_api_keeps_collab_token_when_collab_is_turned_off() {
  render --set collab.enabled=false --set collab.serviceToken=t
  assert_ok
  assert_contains "$(doc Deployment glyph-api)" 'name: COLLAB_SERVICE_TOKEN' "API keeps the collab token while collab is off"
  assert_contains "$(doc Secret glyph-collab)" 'service-token:' "collab Secret is kept while a token is configured"
}

test_api_has_no_collab_token_when_none_is_configured() {
  render --set collab.enabled=false
  assert_ok
  assert_not_contains "$(doc Deployment glyph-api)" 'name: COLLAB_SERVICE_TOKEN' "no token env without a token"
}
