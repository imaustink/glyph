#!/usr/bin/env bash
# Roll production to a commit through Argo CD. Used by CD (.github/workflows/
# cd.yml) and by the manual fallback (scripts/deploy.sh).
#
#   scripts/argocd-deploy.sh COMMIT_SHA IMAGE_TAG
#
# Production is the Argo CD Application `glyph` (homelab repo,
# kubernetes/argocd/glyph/). Its first source is this repo's chart; this
# script points that source at COMMIT_SHA and the three images at IMAGE_TAG
# (the commit's tree hash) in one patch, so the chart (and so the migrations)
# and the binaries always move together. Then it waits for:
#
#   1. Argo's sync of exactly that commit to finish. The migrate Job is a Sync
#      hook in wave -1, so a failed migration fails the sync, and this script
#      fails with it. As with the Helm deploy it replaces, nothing is rolled
#      back automatically (see docs/runbooks/migrations.md): the old pods keep
#      serving.
#   2. Every Deployment of the release to finish rolling out (what
#      `helm upgrade --wait` used to give us).
#
# The Application's spec in git (homelab) leaves targetRevision and the image
# parameters to this script; everything else about it lives there.
#
# Env: ARGOCD_NAMESPACE (argocd), ARGOCD_APP (glyph), APP_NAMESPACE (glyph),
#      REGISTRY (docker.io/blackmarket), DEPLOY_TIMEOUT_SECONDS (600),
#      POLL_SECONDS (5), TRIGGER_AFTER_SECONDS (60), KUBECTL (kubectl).
set -euo pipefail

SHA="${1:?usage: $0 COMMIT_SHA IMAGE_TAG}"
TAG="${2:?usage: $0 COMMIT_SHA IMAGE_TAG}"
ARGOCD_NAMESPACE="${ARGOCD_NAMESPACE:-argocd}"
APP="${ARGOCD_APP:-glyph}"
APP_NAMESPACE="${APP_NAMESPACE:-glyph}"
REGISTRY="${REGISTRY:-docker.io/blackmarket}"
TIMEOUT="${DEPLOY_TIMEOUT_SECONDS:-600}"
POLL="${POLL_SECONDS:-5}"
TRIGGER_AFTER="${TRIGGER_AFTER_SECONDS:-60}"
KUBECTL="${KUBECTL:-kubectl}"
CHART_REPO="https://github.com/imaustink/glyph.git"

log() { echo "▸ $*"; }
die() { echo "✗ $*" >&2; exit 1; }

app() { $KUBECTL -n "$ARGOCD_NAMESPACE" "$@"; }

# "<phase>|<first synced revision>|<startedAt>|<message>" of the app's latest
# sync operation (empty fields when it has none yet).
op_state() {
  app get application "$APP" -o jsonpath='{.status.operationState.phase}|{.status.operationState.syncResult.revisions[0]}|{.status.operationState.startedAt}|{.status.operationState.message}'
}

prev_started="$(op_state | cut -d'|' -f3)"

# One JSON patch: the `test` op makes it fail loudly, instead of retargeting
# the wrong source, if someone reorders the Application's sources.
log "Pointing $APP at $SHA (images $TAG)"
app patch application "$APP" --type json -p "$(cat <<EOF
[
  {"op": "test", "path": "/spec/sources/0/repoURL", "value": "$CHART_REPO"},
  {"op": "replace", "path": "/spec/sources/0/targetRevision", "value": "$SHA"},
  {"op": "add", "path": "/spec/sources/0/helm/parameters", "value": [
    {"name": "frontend.image.repository", "value": "$REGISTRY/glyph-frontend"},
    {"name": "frontend.image.tag", "value": "$TAG"},
    {"name": "api.image.repository", "value": "$REGISTRY/glyph-api"},
    {"name": "api.image.tag", "value": "$TAG"},
    {"name": "collab.image.repository", "value": "$REGISTRY/glyph-collab"},
    {"name": "collab.image.tag", "value": "$TAG"}
  ]}
]
EOF
)"

# Automated sync normally starts on its own once the spec changes. It does
# not when nothing changed — a re-run of CD for the same commit, e.g. after a
# failed sync, which Argo won't retry by itself — so after a while ask for one.
start=$SECONDS
triggered=false
while :; do
  IFS='|' read -r phase rev started message <<<"$(op_state)"
  if [[ -n "$started" && "$started" != "$prev_started" && "$rev" == "$SHA" ]]; then
    case "$phase" in
      Succeeded) log "Sync of $SHA succeeded"; break ;;
      Failed | Error) die "Argo CD sync of $SHA ended $phase: $message" ;;
    esac
  elif ! $triggered && (( SECONDS - start >= TRIGGER_AFTER )); then
    log "No sync of $SHA started on its own; requesting one"
    app patch application "$APP" --type merge \
      -p '{"operation":{"initiatedBy":{"username":"glyph-cd"},"sync":{"syncStrategy":{"hook":{}}}}}'
    triggered=true
  fi
  if (( SECONDS - start >= TIMEOUT )); then
    die "timed out after ${TIMEOUT}s waiting for Argo CD to sync $SHA (last: ${phase:-none} ${rev:-} ${message:-})"
  fi
  sleep "$POLL"
done

remaining=$(( TIMEOUT - (SECONDS - start) ))
(( remaining > 30 )) || remaining=30
log "Waiting for the rollout"
$KUBECTL -n "$APP_NAMESPACE" rollout status deployment \
  -l "app.kubernetes.io/instance=$APP" --timeout "${remaining}s"

echo "✓ $APP is on $SHA"
