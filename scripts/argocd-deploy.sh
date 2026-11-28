#!/usr/bin/env bash
# Roll production to a commit through Argo CD. Used by CD (.github/workflows/
# cd.yml) and by the manual fallback (scripts/deploy.sh).
#
#   scripts/argocd-deploy.sh COMMIT_SHA IMAGE_TAG
#
# Production is the Argo CD Application `glyph` (homelab repo,
# kubernetes/workloads/glyph/argocd/). Its first source is this repo's chart;
# this script points that source at COMMIT_SHA and the three images at
# IMAGE_TAG (the commit's tree hash) in one update, so the chart (and so the
# migrations) and the binaries always move together. Then it waits for:
#
#   1. Argo's sync of exactly that commit to finish. The migrate Job is a Sync
#      hook in wave -1, so a failed migration fails the sync, and this script
#      fails with it. Nothing is rolled back automatically (see
#      docs/runbooks/migrations.md): the old pods keep serving.
#   2. The Application to be Healthy: every Deployment rolled out.
#
# It talks to the Argo CD API with the `argocd` CLI; in CD as the Argo CD
# account glyph-cd, which may only get, update and sync the `glyph`
# Application. No Kubernetes credentials are involved. The homelab repo keeps the rest of the
# Application's spec in git and ignores the two fields set here.
#
# Env: ARGOCD_AUTH_TOKEN: glyph-cd's API token (CD). Without it, the CLI's
#        own login is used, e.g. a person after
#        `argocd login argocd.kurpuis.com --sso --grpc-web` (scripts/deploy.sh).
#      With the token: ARGOCD_SERVER (argocd-server.argocd.svc.cluster.local:80)
#        and ARGOCD_OPTS (--plaintext --grpc-web: the in-cluster Service speaks
#        plain HTTP; TLS ends at Traefik).
#      ARGOCD_APP (glyph),
#      REGISTRY (docker.io/blackmarket), DEPLOY_TIMEOUT_SECONDS (600),
#      POLL_SECONDS (5), TRIGGER_AFTER_SECONDS (60), ARGOCD (argocd).
set -euo pipefail

SHA="${1:?usage: $0 COMMIT_SHA IMAGE_TAG}"
TAG="${2:?usage: $0 COMMIT_SHA IMAGE_TAG}"
if [[ -n "${ARGOCD_AUTH_TOKEN:-}" ]]; then
  export ARGOCD_AUTH_TOKEN
  export ARGOCD_SERVER="${ARGOCD_SERVER:-argocd-server.argocd.svc.cluster.local:80}"
  export ARGOCD_OPTS="${ARGOCD_OPTS:---plaintext --grpc-web}"
fi
APP="${ARGOCD_APP:-glyph}"
REGISTRY="${REGISTRY:-docker.io/blackmarket}"
TIMEOUT="${DEPLOY_TIMEOUT_SECONDS:-600}"
POLL="${POLL_SECONDS:-5}"
TRIGGER_AFTER="${TRIGGER_AFTER_SECONDS:-60}"
ARGOCD="${ARGOCD:-argocd}"
CHART_REPO="https://github.com/imaustink/glyph.git"

log() { echo "▸ $*"; }
die() { echo "✗ $*" >&2; exit 1; }

app_json() { $ARGOCD app get "$APP" -o json; }

# "<phase>|<first synced revision>|<startedAt>|<message>" of the app's latest
# sync operation (empty fields when it has none yet).
op_state() {
  app_json | jq -r '.status.operationState // {} |
    [.phase // "", (.syncResult.revisions // [])[0] // "", .startedAt // "", .message // ""] | join("|")'
}

# Refuse to retarget the wrong source if someone reorders the Application's
# sources: --source-position below is positional.
repo="$(app_json | jq -r '.spec.sources[0].repoURL // ""')"
[[ "$repo" == "$CHART_REPO" ]] || die "$APP's first source is '$repo', not $CHART_REPO; refusing to deploy"

prev_started="$(op_state | cut -d'|' -f3)"

# One update: revision and image parameters together.
log "Pointing $APP at $SHA (images $TAG)"
$ARGOCD app set "$APP" --source-position 1 --revision "$SHA" \
  --helm-set "frontend.image.repository=$REGISTRY/glyph-frontend" \
  --helm-set "frontend.image.tag=$TAG" \
  --helm-set "api.image.repository=$REGISTRY/glyph-api" \
  --helm-set "api.image.tag=$TAG" \
  --helm-set "collab.image.repository=$REGISTRY/glyph-collab" \
  --helm-set "collab.image.tag=$TAG"

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
    $ARGOCD app sync "$APP" --async
    triggered=true
  fi
  if (( SECONDS - start >= TIMEOUT )); then
    die "timed out after ${TIMEOUT}s waiting for Argo CD to sync $SHA (last: ${phase:-none} ${rev:-} ${message:-})"
  fi
  sleep "$POLL"
done

remaining=$(( TIMEOUT - (SECONDS - start) ))
(( remaining > 30 )) || remaining=30
log "Waiting for $APP to be healthy"
$ARGOCD app wait "$APP" --health --timeout "$remaining"

echo "✓ $APP is on $SHA"
