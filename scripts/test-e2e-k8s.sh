#!/usr/bin/env bash
# scripts/test-e2e-k8s.sh — Run Playwright E2E tests against a local Kubernetes
# cluster instead of the Docker Compose stack in scripts/test-e2e.sh.
#
# The cluster is ferry (https://github.com/imaustink/ferry) — a macOS/Apple
# silicon distribution that runs the control plane natively and each pod in
# its own VM. Anything that speaks the Kubernetes API works, though: point
# KUBECONFIG at another cluster and set FERRY=0 to skip the ferry-specific
# bits (see "Using a different cluster" below).
#
# What it does:
#   0. Uses the ferry you have installed. With none on the PATH it installs
#      the latest release (without starting a cluster or registering a login
#      agent). An existing install is never upgraded — if it lacks something
#      this script needs, the script says so and stops.
#   1. Starts a ferry cluster in its own profile (glyph-e2e), with its own
#      state, ports and pod network, so it never touches the default cluster
#      you might develop against. The first run writes the profile's config
#      with `ferry init`: purpose ci, durability process-crash (it is
#      recreated, not precious), machines off, defaultRuntime ferry-vm (every
#      pod its own VM on the Mac — the chart names no RuntimeClass).
#      The cluster is left running between runs; see "Stopping it" below.
#   2. Enables ferry's Traefik addon, the cluster's Ingress controller.
#   3. Builds glyph-api / glyph-frontend-api / glyph-frontend-local /
#      glyph-collab into the node's image store (`ferry image build`, a
#      buildkit pod — no Docker).
#   4. Installs the CloudNativePG operator, which helm/glyph's Cluster needs.
#   5. Installs the chart with e2e/k8s/values.e2e.yaml — collab and the
#      chart's own Ingress on — plus the local-mode frontend and its Ingress
#      from e2e/k8s/frontend-local.yaml.
#   6. Runs Playwright through the Ingress: the api project at
#      http://localhost:<port>, the local project at http://127.0.0.1:<port>.
#      TEST_API_UI_URL / TEST_LOCAL_URL tell playwright.config.ts to start
#      no servers of its own. Nothing is port-forwarded.
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
#   FERRY=0          skip the ferry steps; bring your own cluster, Ingress
#                    controller and the four :e2e images
#   INGRESS_CLASS=.. the IngressClass to use (default: traefik)
#   INGRESS_PORT=..  the port the Ingress controller answers on at localhost
#                    (default: Traefik's node port with ferry, 80 without)
#
# Stopping it:
#   FERRY_PROFILE=glyph-e2e ferry down           # stop; the next run resumes
#   FERRY_PROFILE=glyph-e2e ferry down --purge   # and drop its data
#
# Using a different cluster (kind, k3d, Docker Desktop, a remote cluster):
#   FERRY=0 KUBECONFIG=~/.kube/config INGRESS_CLASS=nginx \
#     ./scripts/test-e2e-k8s.sh
#   …after building the images and loading them however that cluster expects
#   (e.g. `kind load docker-image glyph-api:e2e`), with an Ingress controller
#   reachable at localhost:$INGRESS_PORT. Everything else — the operator, the
#   chart, the Ingresses — is plain Kubernetes.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR/.."
cd "$PROJECT_DIR"

PROJECT="${1:-}"
NAMESPACE="${NAMESPACE:-glyph-e2e}"
RELEASE="glyph"
CNPG_VERSION="1.30.0"
FERRY="${FERRY:-1}"
FERRY_INSTALL_URL="https://get.ferry.kurpuis.com"
INGRESS_CLASS="${INGRESS_CLASS:-traefik}"
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
  # Installing onto a machine with no ferry is harmless. Upgrading one is not
  # this script's call: a release carries its own Kubernetes, so it would move
  # the version under any cluster already running.
  if ! command -v ferry >/dev/null; then
    echo "▶ ferry not found — installing the latest release…"
    curl -sfL "$FERRY_INSTALL_URL" | FERRY_SKIP_START=1 FERRY_SKIP_SERVICE=1 sh -
    # The installer links into /usr/local/bin, else ~/.local/bin, which may
    # not be on this shell's PATH yet.
    export PATH="$PATH:/usr/local/bin:$HOME/.local/bin"
    command -v ferry >/dev/null || { echo "ferry installed, but not found on PATH"; exit 1; }
  fi
  # Check for what this script uses rather than for a version number, so any
  # release that has them works. Both checks run without a cluster.
  # (Captured first: under pipefail, `ferry … | grep -q` can fail on a match,
  # when grep exits early and ferry dies of SIGPIPE.)
  missing=()
  ferry_help="$(ferry help 2>&1 || true)"
  ferry_addons="$(ferry addons list 2>&1 || true)"
  grep -qE '^[[:space:]]+ferry init[[:space:]]' <<<"$ferry_help" || missing+=("ferry init")
  grep -qw traefik <<<"$ferry_addons" || missing+=("the traefik addon")
  if (( ${#missing[@]} )); then
    echo "$(ferry version | head -1) is missing: $(printf '%s, ' "${missing[@]}" | sed 's/, $//'). Upgrade with:"
    echo "  curl -sfL $FERRY_INSTALL_URL | sh -"
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
  # A no-op once enabled; it waits until Traefik actually serves.
  ferry addons enable traefik
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


# ── Ingress ───────────────────────────────────────────────────────────────────
# ferry's Traefik answers on the Mac's port 80 — unless another ferry cluster
# on this Mac already holds it, in which case it says PortInUse (as an Event,
# nowhere a script can check reliably) and answers only on its node port. The
# node port is in this profile's own range, so it is always ours: use it, and
# a second cluster with Traefik on 80 can never quietly take the suite's
# traffic.
if [[ "$FERRY" == "1" && -z "${INGRESS_PORT:-}" ]]; then
  INGRESS_PORT="$(kubectl -n traefik get svc traefik \
    -o jsonpath='{.spec.ports[?(@.name=="web")].nodePort}')"
fi
INGRESS_PORT="${INGRESS_PORT:-80}"

# The api-mode app is the chart's Ingress for host `localhost`; the local-mode
# app takes every other host. Neither name needs DNS or /etc/hosts.
API_UI_ORIGIN="http://localhost:$INGRESS_PORT"
LOCAL_ORIGIN="http://127.0.0.1:$INGRESS_PORT"

echo "▶ Ingress: api-ui=$API_UI_ORIGIN local-ui=$LOCAL_ORIGIN"

# ── Deploy ────────────────────────────────────────────────────────────────────
# The chart reads migrations from helm/glyph/migrations (gitignored); keep it in
# sync with the source of truth, exactly as scripts/deploy.sh does.
rm -rf helm/glyph/migrations
cp -r api/migrations helm/glyph/migrations

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

cleanup() {
  # The EXIT trap's own status becomes the script's status, so a hiccup while
  # tearing down would otherwise mask (or invent) a test failure.
  local status=$?
  if [[ "${KEEP:-}" == "1" ]]; then
    echo "▶ KEEP=1 — namespace $NAMESPACE left running at $API_UI_ORIGIN and $LOCAL_ORIGIN."
    echo "  Clean up with: helm uninstall $RELEASE -n $NAMESPACE && kubectl delete ns $NAMESPACE"
  else
    echo "▶ Tearing down ${NAMESPACE}…"
    helm uninstall "$RELEASE" -n "$NAMESPACE" --wait --timeout 5m >/dev/null 2>&1 || true
    kubectl delete ns "$NAMESPACE" --wait=false >/dev/null 2>&1 || true
  fi
  return "$status"
}
trap cleanup EXIT

echo "▶ Installing chart…"
helm upgrade --install "$RELEASE" helm/glyph \
  -n "$NAMESPACE" \
  -f e2e/k8s/values.e2e.yaml \
  --set "ingress.className=$INGRESS_CLASS" \
  --set "frontend.origin=$API_UI_ORIGIN" \
  --set "api.frontendUrl=$API_UI_ORIGIN" \
  --set "collab.allowedOrigins=$API_UI_ORIGIN" \
  --wait --timeout 10m

echo "▶ Deploying local-mode frontend…"
sed -e "s|__ORIGIN__|$LOCAL_ORIGIN|" -e "s|ingressClassName: traefik|ingressClassName: $INGRESS_CLASS|" \
  e2e/k8s/frontend-local.yaml | kubectl apply -n "$NAMESPACE" -f - >/dev/null
kubectl -n "$NAMESPACE" rollout status deploy/glyph-frontend-local --timeout=5m

# Pods being Ready is not the same as the controller having picked up the
# Ingresses, so wait until each app answers at its own origin.
wait_for_url() {
  local url="$1"
  for _ in $(seq 1 60); do
    if curl -fsS -o /dev/null "$url" 2>/dev/null; then return 0; fi
    sleep 1
  done
  echo "timed out waiting for $url"
  return 1
}
wait_for_url "$API_UI_ORIGIN/health"
wait_for_url "$LOCAL_ORIGIN/"

# ── Run Playwright ────────────────────────────────────────────────────────────
export TEST_API_UI_URL="$API_UI_ORIGIN"
export TEST_LOCAL_URL="$LOCAL_ORIGIN"
EXIT_CODE=0
if [[ -n "$PROJECT" ]]; then
  PLAYWRIGHT_PROJECT="$PROJECT" pnpm exec playwright test --project="$PROJECT" || EXIT_CODE=$?
else
  pnpm exec playwright test || EXIT_CODE=$?
fi

exit "$EXIT_CODE"
