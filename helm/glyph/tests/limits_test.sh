# shellcheck shell=bash
# Low-severity ops findings: request body limits below the API's 5 MiB
# content limit, missing PodDisruptionBudgets, and no preStop on the API.

# _to_bytes 6m → 6291456 (nginx-style size suffixes)
_to_bytes() {
  local v; v="$(printf '%s' "$1" | tr -d '"' | tr '[:upper:]' '[:lower:]')"
  case "$v" in
    *k) echo $(( ${v%k} * 1024 )) ;;
    *m) echo $(( ${v%m} * 1024 * 1024 )) ;;
    *g) echo $(( ${v%g} * 1024 * 1024 * 1024 )) ;;
    *) echo "$v" ;;
  esac
}
MIN_BODY=$((6 * 1024 * 1024))  # 5 MiB content + JSON envelope; MCP accepts 6 MiB

test_nginx_body_limit_covers_content_limit() {
  local v; v="$(sed -n 's/^[[:space:]]*client_max_body_size[[:space:]]*\([^;]*\);.*/\1/p' "$REPO_ROOT/nginx/nginx.conf" | head -1)"
  assert_contains "$v" '.' "nginx.conf sets client_max_body_size (default is 1m)"
  if [[ -n "$v" && "$(_to_bytes "$v")" -lt "$MIN_BODY" ]]; then
    _fail "client_max_body_size $v is below 6m"
  fi
}

test_ingress_sets_nginx_body_size_by_default() {
  render --set ingress.enabled=true
  assert_ok
  local i; i="$(doc Ingress glyph)"
  local v; v="$(printf '%s' "$i" | sed -n 's/.*nginx.ingress.kubernetes.io\/proxy-body-size: \(.*\)/\1/p')"
  assert_contains "$v" '.' "ingress-nginx proxy-body-size annotation set by default"
  if [[ -n "$v" && "$(_to_bytes "$v")" -lt "$MIN_BODY" ]]; then
    _fail "proxy-body-size $v is below 6m"
  fi
}

test_frontend_body_size_limit_env() {
  render
  assert_ok
  local d; d="$(doc Deployment glyph-frontend)"
  assert_contains "$d" 'name: BODY_SIZE_LIMIT' "SvelteKit's 512K default would reject proxied saves"
  local v; v="$(printf '%s\n' "$d" | grep -A1 'name: BODY_SIZE_LIMIT' | sed -n 's/.*value: //p' || true)"
  if [[ -n "$v" && "$(_to_bytes "$v")" -lt "$MIN_BODY" ]]; then
    _fail "BODY_SIZE_LIMIT $v is below 6M"
  fi
}

test_pdbs_for_replicated_components() {
  render --set collab.enabled=true --set collab.serviceToken=t --set collab.replicaCount=2
  assert_ok
  local c
  for c in api frontend collab; do
    local p; p="$(doc PodDisruptionBudget "glyph-$c")"
    assert_contains "$p" 'kind: PodDisruptionBudget' "PDB for $c"
    assert_contains "$p" 'maxUnavailable: 1' "$c PDB allows one pod down at a time"
    assert_contains "$p" "app.kubernetes.io/component: $c" "$c PDB selects $c pods"
  done
}

test_no_pdb_for_single_replica() {
  render --set collab.enabled=true --set collab.serviceToken=t --set api.replicaCount=1
  assert_ok
  assert_not_contains "$(doc PodDisruptionBudget glyph-collab)" 'kind' "no PDB for single-replica collab (it would block drains)"
  assert_not_contains "$(doc PodDisruptionBudget glyph-api)" 'kind' "no PDB for single-replica api"
}

test_api_prestop_sleep() {
  render
  assert_ok
  local d; d="$(doc Deployment glyph-api)"
  assert_contains "$d" 'preStop:' "API has a preStop hook"
  # distroless image: no shell or sleep binary, so the native sleep action.
  assert_contains "$d" 'sleep:' "uses the lifecycle sleep action"
  assert_contains "$d" 'seconds: 5' "sleeps 5s"
}
