#!/usr/bin/env bash
# scripts/test-e2e-k8s.sh — Run Playwright E2E tests against a local Kubernetes
# cluster instead of the Docker Compose stack in scripts/test-e2e.sh.
#
# The cluster is ferry (https://github.com/imaustink/ferry) v0.9 or newer — a
# macOS/Apple silicon distribution that runs the control plane natively and
# each pod in its own VM. Anything that speaks the Kubernetes API works,
# though: point KUBECONFIG at another cluster and set FERRY=0 to skip the
# ferry-specific bits (see "Using a different cluster" below).
#
# What it does:
#   1. Starts a ferry cluster in its own profile (glyph-e2e), with its own
#      state, ports and pod network, so it never touches the default cluster
#      you might develop against. The first run writes the profile's config
#      with `ferry init`: purpose ci, durability process-crash (it is
#      recreated, not precious), machines off, defaultRuntime ferry-vm (every
#      pod its own VM on the Mac — the chart names no RuntimeClass).
#      The cluster is left running between runs; see "Stopping it" below.
#   2. Builds glyph-api / glyph-frontend-api / glyph-frontend-local /
#      glyph-collab into the node's image store (`ferry image build`, a
#      buildkit pod — no Docker).
#   3. Installs the CloudNativePG operator, which helm/glyph's Cluster needs.
#   4. Installs the chart with e2e/k8s/values.e2e.yaml (collab on), plus the
#      local-mode frontend from e2e/k8s/frontend-local.yaml and the
#      same-origin edge proxy from e2e/k8s/edge.yaml.
#   5. Forwards the Services to loopback ports and runs Playwright against
#      them. Playwright's `reuseExistingServer` sees the forwards already
#      listening and starts nothing of its own.
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
#   FERRY_PROFILE=.. the ferry profile to run in (default: glyph-e2e)
#   FERRY=0          skip `ferry up` / `ferry image build`; bring your own
#                    cluster and load the four :e2e images into it yourself
#
# Stopping it:
#   FERRY_PROFILE=glyph-e2e ferry down           # stop; the next run resumes
#   FERRY_PROFILE=glyph-e2e ferry down --purge   # and drop its data
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
FERRY_MIN_VERSION="0.9.0"
export FERRY_PROFILE="${FERRY_PROFILE:-glyph-e2e}"

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
    echo "  curl -sfL https://get.ferry.kurpuis.com | FERRY_VERSION=v${FERRY_MIN_VERSION} sh -"
    exit 1
  }
  # 0.9 is the first with `ferry init`/`ferry config` and RuntimeClasses, and
  # the first whose memory-backed emptyDir is a real tmpfs.
  ferry_version="$(ferry version | awk 'NR==1 {sub(/^v/, "", $2); print $2}')"
  if [[ "$(printf '%s\n%s\n' "$FERRY_MIN_VERSION" "$ferry_version" | sort -V | head -1)" != "$FERRY_MIN_VERSION" ]]; then
    echo "ferry $ferry_version is too old; this needs v$FERRY_MIN_VERSION or newer. Upgrade with:"
    echo "  curl -sfL https://get.ferry.kurpuis.com | FERRY_VERSION=v${FERRY_MIN_VERSION} sh -"
    exit 1
  fi
  command -v buildctl >/dev/null || {
    echo "buildctl not found (ferry runs the builder, the client comes from buildkit):"
    echo "  brew install buildkit"
    exit 1
  }
  # The profile's config is written once and then left alone, so anything
  # changed with `ferry config set` sticks across runs.
  if [[ ! -f "$(ferry config path)" ]]; then
    ferry init --purpose ci --machines false --default-runtime ferry-vm --yes
  fi
  export KUBECONFIG="$(ferry kubeconfig)"
  # `ferry up` exits 1 against an already-running cluster rather than no-opping,
  # and `ferry status` exits 0 whether or not one is running — so ask the API
  # server itself.
  if ! kubectl get --raw /readyz >/dev/null 2>&1; then
    ferry up
  fi
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
  ferry image build -t glyph-collab:e2e -f collab/Dockerfile .
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
export TEST_COLLAB_PORT="$(free_port)"

API_UI_ORIGIN="http://localhost:$TEST_API_UI_PORT"
LOCAL_ORIGIN="http://localhost:$TEST_LOCAL_PORT"

echo "▶ Ports: api=$TEST_API_PORT collab=$TEST_COLLAB_PORT local-ui=$TEST_LOCAL_PORT api-ui=$TEST_API_UI_PORT"

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
  --set "collab.allowedOrigins=$API_UI_ORIGIN" \
  --wait --timeout 10m

echo "▶ Deploying edge proxy…"
kubectl apply -n "$NAMESPACE" -f e2e/k8s/edge.yaml >/dev/null
# nginx reads its config once, so a namespace kept from an earlier run
# (KEEP=1) would go on serving the old one.
kubectl -n "$NAMESPACE" rollout restart deploy/glyph-edge >/dev/null
kubectl -n "$NAMESPACE" rollout status deploy/glyph-edge --timeout=5m

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
  # The browser goes through the edge proxy, so /collab is same-origin. The
  # collab Service is forwarded as well only so Playwright's collab webServer
  # entry finds its health check answered and starts nothing.
  forward glyph-edge "$TEST_API_UI_PORT" 8080
  wait_for_port "$TEST_API_UI_PORT" "glyph-edge"
  forward glyph-collab "$TEST_COLLAB_PORT" 1235
  wait_for_port "$TEST_COLLAB_PORT" "glyph-collab"
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
