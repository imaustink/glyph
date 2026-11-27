#!/usr/bin/env bash
# Restart a PR preview's pods that are stuck waiting for an image that now
# exists. Run by CI (.github/workflows/ci.yml, restart-preview-pods) right
# after build-preview-image pushes sha-<head commit>.
#
#   scripts/preview-restart-stuck-pods.sh PR_NUMBER
#
# Why: Argo CD's pull-request generator renders a preview with image tag
# sha-<head commit> as soon as it sees the PR (on labeling, or a new push),
# which is often before CI has pushed that image. The pods then sit in
# ImagePullBackOff, and the kubelet's back-off (up to 5 minutes) delays them
# long after the image exists; meanwhile Traefik serves its default
# certificate for a brand-new preview. Argo has nothing left to do (the
# manifests are applied), so deleting those pods is the whole fix: their
# controllers (Deployment, or the migrate/seed Job) recreate them and they
# pull at once.
#
# Pods that are still being created when the image lands can fail their pull
# a moment later, so it looks a few times. Doing nothing is fine: no preview
# yet (Argo will deploy it with the image already there) or nothing stuck.
#
# Env: PREVIEW_NAMESPACE (glyph-preview), ROUNDS (3), ROUND_SECONDS (20),
#      KUBECTL (kubectl).
set -euo pipefail

PR="${1:?usage: $0 PR_NUMBER}"
[[ "$PR" =~ ^[0-9]+$ ]] || { echo "not a PR number: $PR" >&2; exit 2; }
NS="${PREVIEW_NAMESPACE:-glyph-preview}"
ROUNDS="${ROUNDS:-3}"
ROUND_SECONDS="${ROUND_SECONDS:-20}"
KUBECTL="${KUBECTL:-kubectl}"
SELECTOR="app.kubernetes.io/name=glyph,app.kubernetes.io/instance=glyph-pr-$PR"

# One line per pod: name, then every container's waiting reason.
JSONPATH='{range .items[*]}{.metadata.name}{" "}{.status.initContainerStatuses[*].state.waiting.reason}{" "}{.status.containerStatuses[*].state.waiting.reason}{"\n"}{end}'

restarted=0
for ((round = 1; round <= ROUNDS; round++)); do
  pods="$("$KUBECTL" -n "$NS" get pods -l "$SELECTOR" -o jsonpath="$JSONPATH")"
  stuck=()
  while read -r name reasons; do
    [[ -z "$name" ]] && continue
    if [[ " $reasons " =~ \ (ErrImagePull|ImagePullBackOff)\  ]]; then
      stuck+=("$name")
    fi
  done <<<"$pods"

  if ((${#stuck[@]})); then
    echo "restarting pods stuck pulling images: ${stuck[*]}"
    "$KUBECTL" -n "$NS" delete pod --wait=false "${stuck[@]}"
    restarted=$((restarted + ${#stuck[@]}))
  fi
  ((round < ROUNDS)) && sleep "$ROUND_SECONDS"
done

echo "pr-$PR: restarted $restarted pod(s)"
