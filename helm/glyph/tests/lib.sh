# shellcheck shell=bash
# Helpers for the chart tests. Sourced by run.sh — see there for usage.
#
# Every test renders a throwaway copy of the chart (with api/migrations synced
# in, as cd.yml does at deploy time) so the working tree is never touched.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CHART_SRC="$REPO_ROOT/helm/glyph"

_setup_chart() {
  WORK="$(mktemp -d "${TMPDIR:-/tmp}/glyph-chart-test.XXXXXX")"
  CHART="$WORK/glyph"
  cp -R "$CHART_SRC" "$CHART"
  rm -rf "$CHART/tests" "$CHART/migrations"
  mkdir -p "$CHART/migrations"
  cp "$REPO_ROOT"/api/migrations/*.sql "$CHART/migrations/"
}

# render [helm template args...]
#   Renders the chart as release "glyph" (the production release name) and
#   leaves stdout+stderr in $OUT and the exit status in $STATUS.
render() {
  set +e
  OUT="$(helm template glyph "$CHART" "$@" 2>&1)"
  STATUS=$?
  set -e
}

# doc KIND NAME — prints the rendered document with that kind and
# metadata.name from $OUT (empty if none).
doc() {
  printf '%s\n' "$OUT" | awk -v kind="$1" -v name="$2" '
    function flush() {
      if (buf != "" && k == kind && n == name) printf "%s", buf
      buf = ""; k = ""; n = ""; inmeta = 0
    }
    /^---/ { flush(); next }
    {
      buf = buf $0 "\n"
      if ($0 ~ /^kind: /) { k = $2 }
      if ($0 ~ /^metadata:/) { inmeta = 1; next }
      if (inmeta && $0 ~ /^  name: /) { n = $2; gsub(/"/, "", n); inmeta = 0 }
      if ($0 ~ /^[^ ]/) { inmeta = 0 }
    }
    END { flush() }'
}

# has_kind KIND — true if any rendered doc has that kind.
has_kind() { printf '%s\n' "$OUT" | grep -qE "^kind: $1\$"; }

_fail() {
  FAILED=1
  printf '    ✗ %s\n' "$*" >&2
}

assert_ok() {
  if [[ "$STATUS" -ne 0 ]]; then
    _fail "expected render to succeed, got status $STATUS: $(printf '%s' "$OUT" | grep -m3 -i error)"
  fi
}

# assert_render_fails PATTERN — render must fail with PATTERN in its output.
assert_render_fails() {
  if [[ "$STATUS" -eq 0 ]]; then
    _fail "expected render to fail with /$1/, but it succeeded"
  elif ! printf '%s' "$OUT" | grep -qE -- "$1"; then
    _fail "render failed, but not with /$1/: $(printf '%s' "$OUT" | grep -m3 -i error)"
  fi
}

# assert_contains TEXT PATTERN [description]  (PATTERN is an ERE)
assert_contains() {
  if ! printf '%s' "$1" | grep -qE -- "$2"; then
    _fail "${3:-expected match} — /$2/ not found"
  fi
}

assert_not_contains() {
  if printf '%s' "$1" | grep -qE -- "$2"; then
    _fail "${3:-expected no match} — /$2/ found: $(printf '%s' "$1" | grep -m1 -E -- "$2")"
  fi
}

assert_eq() {
  if [[ "$1" != "$2" ]]; then
    _fail "${3:-values differ} — expected '$2', got '$1'"
  fi
}
