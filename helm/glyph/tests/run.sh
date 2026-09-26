#!/usr/bin/env bash
# Chart and deploy-pipeline tests: `make test-helm`.
#
# Each *_test.sh file in this directory defines `test_*` functions. Every test
# gets a fresh copy of the chart and uses the helpers in lib.sh — `render`
# (helm template), `doc KIND NAME` and the assert_* functions. A test fails if
# any assertion in it fails.
#
#   helm/glyph/tests/run.sh              # all tests
#   helm/glyph/tests/run.sh backup       # only tests whose name matches
#
# Needs helm (v3) and bash; nothing else.
set -euo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$TESTS_DIR/lib.sh"
for f in "$TESTS_DIR"/*_test.sh; do
  # shellcheck disable=SC1090
  source "$f"
done

FILTER="${1:-}"
PASS=0
FAILURES=()

while read -r name; do
  [[ -n "$FILTER" && "$name" != *"$FILTER"* ]] && continue
  _setup_chart
  FAILED=0
  "$name"
  rm -rf "$WORK"
  if [[ "$FAILED" -eq 0 ]]; then
    PASS=$((PASS + 1))
    printf '  ✓ %s\n' "$name"
  else
    FAILURES+=("$name")
    printf '  ✗ %s\n' "$name"
  fi
done < <(declare -F | awk '{print $3}' | grep '^test_' | sort)

echo
echo "$PASS passed, ${#FAILURES[@]} failed"
if [[ ${#FAILURES[@]} -gt 0 ]]; then
  printf '  FAIL %s\n' "${FAILURES[@]}"
  exit 1
fi
