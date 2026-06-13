#!/usr/bin/env bash
set -euo pipefail

REGISTRY="registry.kurpuis.com:5000"
FRONTEND_IMAGE="$REGISTRY/glyph-frontend:latest"
API_IMAGE="$REGISTRY/glyph-api:latest"
NAMESPACE="glyph"

# Parse flags
BUILD_FRONTEND=true
BUILD_API=true
SKIP_PUSH=false
SKIP_DEPLOY=false

usage() {
  echo "Usage: $0 [--frontend-only | --api-only] [--skip-push] [--skip-deploy]"
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --frontend-only) BUILD_API=false; shift ;;
    --api-only) BUILD_FRONTEND=false; shift ;;
    --skip-push) SKIP_PUSH=true; shift ;;
    --skip-deploy) SKIP_DEPLOY=true; shift ;;
    -h|--help) usage ;;
    *) echo "Unknown option: $1"; usage ;;
  esac
done

cd "$(git rev-parse --show-toplevel)"

# ── Sync migrations into Helm chart ───────────────────────────────────────────
echo "▸ Syncing migrations..."
rm -rf helm/glyph/migrations
cp -r api/migrations helm/glyph/migrations

# ── Build ──────────────────────────────────────────────────────────────────────
if $BUILD_FRONTEND; then
  echo "▸ Building frontend..."
  docker build \
    --build-arg VITE_STORAGE_MODE=api \
    --build-arg VITE_API_URL= \
    -t "$FRONTEND_IMAGE" \
    -f Dockerfile .
fi

if $BUILD_API; then
  echo "▸ Building API..."
  docker build \
    -t "$API_IMAGE" \
    -f api/Dockerfile api/
fi

# ── Push ───────────────────────────────────────────────────────────────────────
if ! $SKIP_PUSH; then
  if $BUILD_FRONTEND; then
    echo "▸ Pushing frontend..."
    docker push "$FRONTEND_IMAGE"
  fi
  if $BUILD_API; then
    echo "▸ Pushing API..."
    docker push "$API_IMAGE"
  fi
fi

# ── Deploy ─────────────────────────────────────────────────────────────────────
if ! $SKIP_DEPLOY; then
  echo "▸ Upgrading Helm release..."
  helm upgrade glyph helm/glyph -f values-production.yaml -n "$NAMESPACE" --wait --timeout 5m

  DEPLOYMENTS=()
  if $BUILD_FRONTEND; then DEPLOYMENTS+=("deploy/glyph-frontend"); fi
  if $BUILD_API; then DEPLOYMENTS+=("deploy/glyph-api"); fi

  echo "▸ Restarting ${DEPLOYMENTS[*]}..."
  kubectl rollout restart "${DEPLOYMENTS[@]}" -n "$NAMESPACE"
  kubectl rollout status "${DEPLOYMENTS[@]}" -n "$NAMESPACE" --timeout=3m
fi

echo "✓ Done"
