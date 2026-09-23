#!/usr/bin/env bash
# scripts/test-e2e-k8s.sh — Run Playwright E2E tests against a local Kubernetes
# cluster instead of the Docker Compose stack in scripts/test-e2e.sh.
#
# The cluster is ferry (https://github.com/imaustink/ferry) — a macOS/Apple
# silicon distribution that runs the control plane natively and each pod in its
# own VM. Anything that speaks the Kubernetes API works, though: point
# KUBECONFIG at another cluster and set FERRY=0 to skip the ferry-specific
# bits (see "Using a different cluster" below).
#
# What it does:
#   1. Builds glyph-api / glyph-frontend-api / glyph-frontend-local into the
#      node's image store (`ferry image build`, a buildkit pod — no Docker).
#   2. Installs the CloudNativePG operator, which helm/glyph's Cluster needs.
#   3. Installs the chart with e2e/k8s/values.e2e.yaml plus the local-mode
#      frontend from e2e/k8s/frontend-local.yaml.
#   4. Forwards the three Services to loopback ports and runs Playwright
#      against them. Playwright's `reuseExistingServer` sees the forwards
#      already listening and starts nothing of its own.
#
# Usage:
#   ./scripts/test-e2e-k8s.sh            # both projects (local + api)
#   ./scripts/test-e2e-k8s.sh api        # api project only
#   ./scripts/test-e2e-k8s.sh local      # local project only
#
# Environment:
#   SKIP_BUILD=1     reuse the :e2e images already in the node's image store
#   KEEP=1           leave the namespace running after the tests (to debug)
#   NAMESPACE=...    override the namespace (default: glyph-e2e)
#   FERRY=0          skip `ferry up` / `ferry image build`; bring your own
#                    cluster and load the three :e2e images into it yourself
#
# Using a different cluster (kind, k3d, Docker Desktop, a remote cluster):
#   FERRY=0 KUBECONFIG=~/.kube/config ./scripts/test-e2e-k8s.sh
#   …after building the images and loading them however that cluster expects
#   (e.g. `kind load docker-image glyph-api:e2e`). Everything else — the
#   operator, the chart, the port-forwards — is plain Kubernetes.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR/.."
cd "$PROJECT_DIR"

PROJECT="${1:-}"
NAMESPACE="${NAMESPACE:-glyph-e2e}"
RELEASE="glyph"
CNPG_VERSION="1.30.0"
FERRY="${FERRY:-1}"

# ── Preflight ─────────────────────────────────────────────────────────────────
# Everything the app needs runs in the cluster; the only thing needed locally is
# Playwright itself. Check for it up front — `pnpm exec` reports a missing
# install as a workspace error, which sends you looking in the wrong place.
[[ -x node_modules/.bin/playwright ]] || {
  echo "Playwright is not installed in this checkout. Run:"
  echo "  pnpm install --frozen-lockfile && pnpm exec playwright install chromium"
  exit 1
}

# ── Cluster ───────────────────────────────────────────────────────────────────
if [[ "$FERRY" == "1" ]]; then
  command -v ferry >/dev/null || {
    echo "ferry not found. Install it with:"
    echo "  curl -sfL https://get.ferry.kurpuis.com | FERRY_VERSION=v0.5.0 sh -"
    exit 1
  }
  command -v buildctl >/dev/null || {
    echo "buildctl not found (ferry runs the builder, the client comes from buildkit):"
    echo "  brew install buildkit"
    exit 1
  }
  # `ferry up` exits 1 against an already-running cluster rather than no-opping,
  # so only start one when `ferry status` says there isn't one.
  ferry status >/dev/null 2>&1 || ferry up
  export KUBECONFIG="${KUBECONFIG:-$HOME/.ferry-current/admin.conf}"
fi

kubectl cluster-info >/dev/null 2>&1 || {
  echo "no reachable cluster — check KUBECONFIG (${KUBECONFIG:-~/.kube/config})"
  exit 1
}

echo "▶ Cluster: $(kubectl config current-context) — namespace $NAMESPACE"

# ── Build images into the node's image store ──────────────────────────────────
# These never reach a registry, so every pod spec uses imagePullPolicy: Never.
if [[ "${SKIP_BUILD:-}" != "1" && "$FERRY" == "1" ]]; then
  echo "▶ Building images…"
  ferry image build -t glyph-api:e2e -f api/Dockerfile api/
  ferry image build -t glyph-frontend-api:e2e -f Dockerfile \
    --build-arg VITE_STORAGE_MODE=api --build-arg VITE_API_URL= .
  ferry image build -t glyph-frontend-local:e2e -f Dockerfile \
    --build-arg VITE_STORAGE_MODE=local .
  # The builder is a pod, and it reserves several GiB so buildkit can actually
  # use them. Left running it is enough on its own to put the node under
  # memory-pressure, which taints it NoSchedule and leaves the whole release
  # Pending. Nothing below needs the builder, so stop it before deploying.
  ferry image build --stop
fi

# ── CloudNativePG operator ────────────────────────────────────────────────────
# helm/glyph ships a postgresql.cnpg.io Cluster; the CRD and its controller are
# a cluster-wide prerequisite the chart does not (and should not) install.
if ! kubectl get deploy cnpg-controller-manager -n cnpg-system >/dev/null 2>&1; then
  echo "▶ Installing CloudNativePG ${CNPG_VERSION}…"
  kubectl apply --server-side -f \
    "https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-${CNPG_VERSION%.*}/releases/cnpg-${CNPG_VERSION}.yaml" >/dev/null
fi
kubectl -n cnpg-system rollout status deploy/cnpg-controller-manager --timeout=300s

# ── Allocate host ports for this run ──────────────────────────────────────────
# Same trick as scripts/test-e2e.sh: ask the OS for a free ephemeral port and
# release it. Ports are picked before `helm install` because ORIGIN/FRONTEND_URL
# are baked into the deployment's environment.
free_port() {
  node -e "const s=require('net').createServer();s.listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"
}
export TEST_API_PORT="$(free_port)"
export TEST_LOCAL_PORT="$(free_port)"
export TEST_API_UI_PORT="$(free_port)"

API_UI_ORIGIN="http://localhost:$TEST_API_UI_PORT"
LOCAL_ORIGIN="http://localhost:$TEST_LOCAL_PORT"

echo "▶ Ports: api=$TEST_API_PORT local-ui=$TEST_LOCAL_PORT api-ui=$TEST_API_UI_PORT"

# ── Deploy ────────────────────────────────────────────────────────────────────
# The chart reads migrations from helm/glyph/migrations (gitignored); keep it in
# sync with the source of truth, exactly as scripts/deploy.sh does.
rm -rf helm/glyph/migrations
cp -r api/migrations helm/glyph/migrations

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

echo "▶ Installing chart…"
helm upgrade --install "$RELEASE" helm/glyph \
  -n "$NAMESPACE" \
  -f e2e/k8s/values.e2e.yaml \
  --set "frontend.origin=$API_UI_ORIGIN" \
  --set "api.frontendUrl=$API_UI_ORIGIN" \
  --wait --timeout 10m

echo "▶ Deploying local-mode frontend…"
sed "s|__ORIGIN__|$LOCAL_ORIGIN|" e2e/k8s/frontend-local.yaml | kubectl apply -n "$NAMESPACE" -f - >/dev/null
kubectl -n "$NAMESPACE" rollout status deploy/glyph-frontend-local --timeout=5m

# ── Port-forward ──────────────────────────────────────────────────────────────
# kubectl port-forward drops its tunnel on an upstream reset, which a 20-minute
# suite will hit. Each forward runs in a respawn loop and is torn down on exit.
PF_PIDS=()

cleanup() {
  # The EXIT trap's own status becomes the script's status, so a hiccup while
  # tearing down would otherwise mask (or invent) a test failure.
  local status=$?
  for pid in "${PF_PIDS[@]:-}"; do
    # Kill the respawn loop first, then the kubectl it is currently supervising
    # — killing only the subshell leaves an orphaned port-forward holding the
    # port.
    kill "$pid" 2>/dev/null || true
    pkill -P "$pid" 2>/dev/null || true
  done
  if [[ "${KEEP:-}" == "1" ]]; then
    echo "▶ KEEP=1 — namespace $NAMESPACE left running."
    echo "  Clean up with: helm uninstall $RELEASE -n $NAMESPACE && kubectl delete ns $NAMESPACE"
  else
    echo "▶ Tearing down ${NAMESPACE}…"
    helm uninstall "$RELEASE" -n "$NAMESPACE" --wait --timeout 5m >/dev/null 2>&1 || true
    kubectl delete ns "$NAMESPACE" --wait=false >/dev/null 2>&1 || true
  fi
  return "$status"
}
trap cleanup EXIT

forward() {
  local svc="$1" host_port="$2" svc_port="$3"
  (
    # Kill the kubectl this loop is supervising when the loop itself is
    # signalled. Without the trap, terminating the subshell just reparents
    # kubectl to init and it keeps holding the port after the run.
    kpid=""
    trap 'kill "$kpid" 2>/dev/null; exit 0' TERM INT
    while true; do
      kubectl -n "$NAMESPACE" port-forward "svc/$svc" "$host_port:$svc_port" >/dev/null 2>&1 &
      kpid=$!
      wait "$kpid" || true
      sleep 1
    done
  ) &
  PF_PIDS+=($!)
}

wait_for_port() {
  local port="$1" name="$2"
  for _ in $(seq 1 60); do
    if nc -z 127.0.0.1 "$port" 2>/dev/null; then return 0; fi
    sleep 1
  done
  echo "timed out waiting for $name on port $port"
  return 1
}

echo "▶ Forwarding services…"
forward glyph-api "$TEST_API_PORT" 8080
wait_for_port "$TEST_API_PORT" "glyph-api"

if [[ -z "$PROJECT" || "$PROJECT" == "api" ]]; then
  forward glyph-frontend "$TEST_API_UI_PORT" 3000
  wait_for_port "$TEST_API_UI_PORT" "glyph-frontend"
fi

if [[ -z "$PROJECT" || "$PROJECT" == "local" ]]; then
  forward glyph-frontend-local "$TEST_LOCAL_PORT" 3000
  wait_for_port "$TEST_LOCAL_PORT" "glyph-frontend-local"
fi

# ── Run Playwright ────────────────────────────────────────────────────────────
# REUSE_API_SERVER makes the api webServer entry reuse whatever already listens
# on TEST_API_PORT (the forward) instead of running `go run ./cmd/api` locally;
# the two frontend entries reuse by default outside CI.
EXIT_CODE=0
if [[ -n "$PROJECT" ]]; then
  REUSE_API_SERVER=true PLAYWRIGHT_PROJECT="$PROJECT" \
    pnpm exec playwright test --project="$PROJECT" || EXIT_CODE=$?
else
  REUSE_API_SERVER=true pnpm exec playwright test || EXIT_CODE=$?
fi

exit "$EXIT_CODE"
