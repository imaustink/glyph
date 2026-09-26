# shellcheck shell=bash
# Whole-chart checks.

test_chart_lints() {
  set +e
  OUT="$(helm lint "$CHART" --set api.sessionSecret=00 --set collab.enabled=true \
    --set collab.serviceToken=t 2>&1)"
  STATUS=$?
  set -e
  assert_ok
}
