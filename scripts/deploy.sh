#!/usr/bin/env bash
# Manual deploy of the tip of origin/main — a fallback for when CD
# (.github/workflows/cd.yml) can't run. Prefer re-running CD:
#
#   gh workflow run cd.yml --ref main
#
# This does what CD does, with the same guard rails:
#   * it only ships the exact tip of origin/main, from a clean tree, so it can
#     never roll production back to an older schema than it already has;
#   * it refuses while a CD run is queued or in progress (it can't take part
#     in CD's concurrency lock, so it checks instead — needs `gh`);
#   * images are tagged by git tree hash, like CD, so the tag changes with
#     the content and pullPolicy IfNotPresent can't serve a stale :latest;
#   * `helm upgrade --wait`: the pre-upgrade migrate hook runs first, and any
#     failure fails the deploy. There is deliberately no --atomic: the
#     migrations have already applied, and rolling back to a revision from
#     before PR #46 crash-loops the API (its init container runs `migrate up`
#     against the newer schema). A failed rollout leaves the old pods
#     serving; see docs/runbooks/migrations.md to recover.
set -euo pipefail

REGISTRY="docker.io/blackmarket"
NAMESPACE="glyph"
RELEASE="glyph"
VALUES_FILE="values-production.yaml"
BUILD_ONLY=false

usage() {
  echo "Usage: $0 [--values FILE] [--build-only]"
  echo "  --values FILE   production values file (default: $VALUES_FILE)"
  echo "  --build-only    build and push any missing images; don't deploy"
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --values) VALUES_FILE="${2:?}"; shift 2 ;;
    --build-only) BUILD_ONLY=true; shift ;;
    -h|--help) usage ;;
    *) echo "Unknown option: $1"; usage ;;
  esac
done

die() { echo "✗ $*" >&2; exit 1; }

cd "$(git rev-parse --show-toplevel)"

# ── Guards ────────────────────────────────────────────────────────────────────
if [[ -n "$(git status --porcelain)" ]]; then
  die "the working tree has uncommitted changes; deploy only what is on origin/main."
fi
echo "▸ Fetching origin/main..."
git fetch -q origin main
head="$(git rev-parse HEAD)"
tip="$(git rev-parse origin/main)"
if [[ "$head" != "$tip" ]]; then
  die "HEAD ($head) is not the tip of origin/main ($tip). Check out origin/main first."
fi

if ! $BUILD_ONLY; then
  if command -v gh >/dev/null 2>&1; then
    active="$(gh run list --workflow cd.yml --limit 20 --json status --jq '.[].status' 2>/dev/null \
      | grep -E '^(in_progress|queued|waiting|pending|requested)$' || true)"
    if [[ -n "$active" ]]; then
      die "a CD run is queued or in progress; let it finish (or cancel it) instead of racing it."
    fi
  else
    echo "! gh not found — can't check for a running CD deploy. Make sure none is running."
  fi
fi

TAG="$(git rev-parse 'HEAD^{tree}')"
echo "▸ Deploying tree $TAG (commit $head)"

# ── Build & push any image missing from the registry (same matrix as CD) ──────
build_if_missing() {
  local image="$1" dockerfile="$2" context="$3"; shift 3
  local ref="$REGISTRY/glyph-$image:$TAG"
  if docker manifest inspect "$ref" >/dev/null 2>&1; then
    echo "▸ $ref already in registry"
    return
  fi
  echo "▸ Building $ref..."
  docker build "$@" -t "$ref" -f "$dockerfile" "$context"
  docker push "$ref"
}
build_if_missing frontend Dockerfile . --build-arg VITE_STORAGE_MODE=api --build-arg VITE_API_URL=
build_if_missing api api/Dockerfile api/
build_if_missing collab collab/Dockerfile .

$BUILD_ONLY && { echo "✓ Images pushed"; exit 0; }

# ── Deploy ─────────────────────────────────────────────────────────────────────
[[ -f "$VALUES_FILE" ]] || die "values file $VALUES_FILE not found (--values FILE)."

echo "▸ Syncing migrations..."
rm -rf helm/glyph/migrations
cp -r api/migrations helm/glyph/migrations

echo "▸ Upgrading Helm release..."
helm upgrade "$RELEASE" helm/glyph \
  -f "$VALUES_FILE" \
  --set frontend.image.repository="$REGISTRY/glyph-frontend" \
  --set frontend.image.tag="$TAG" \
  --set api.image.repository="$REGISTRY/glyph-api" \
  --set api.image.tag="$TAG" \
  --set collab.image.repository="$REGISTRY/glyph-collab" \
  --set collab.image.tag="$TAG" \
  -n "$NAMESPACE" --wait --cleanup-on-fail --timeout 10m

echo "✓ Done"
