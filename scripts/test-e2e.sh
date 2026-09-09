#!/usr/bin/env bash
# scripts/test-e2e.sh — Run Playwright E2E tests against an isolated Docker stack.
#
# Every resource this script touches (Postgres port, Go API port, the two
# Vite dev-server ports, and the Docker Compose project name/network/volumes)
# is allocated fresh for THIS invocation — nothing is hardcoded. That means
# multiple agents/worktrees can each run the full E2E suite on the same
# machine at the same time without stepping on each other's containers or
# ports. Waits until the stack is healthy, runs the requested Playwright
# project(s), then tears everything down — even on failure.
#
# Usage:
#   ./scripts/test-e2e.sh            # all projects
#   ./scripts/test-e2e.sh api        # api project only
#   ./scripts/test-e2e.sh local      # local project only

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR/.."
COMPOSE_FILE="$PROJECT_DIR/docker-compose.test.yml"
PROJECT="${1:-}"

# Always run from the project root so pnpm finds package.json
cd "$PROJECT_DIR"

# ── Allocate isolated resources for this run ───────────────────────────────────
# Ask the OS for a free ephemeral port, then release it immediately. There's a
# small TOCTOU race (another process could grab it before we bind), but it's
# the same tradeoff every "find a free port" helper makes and is good enough
# for local/CI test isolation.
free_port() {
  node -e "const s=require('net').createServer();s.listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})"
}

export TEST_PG_PORT="$(free_port)"
export TEST_API_PORT="$(free_port)"
export TEST_LOCAL_PORT="$(free_port)"
export TEST_API_UI_PORT="$(free_port)"

# Unique per run (not just per worktree) so two concurrent invocations in the
# same worktree also get separate containers/networks/volumes.
export COMPOSE_PROJECT_NAME="glyph-test-$$-$(date +%s%N 2>/dev/null || date +%s)"

echo "▶ Isolated run: project=$COMPOSE_PROJECT_NAME pg=$TEST_PG_PORT api=$TEST_API_PORT local-ui=$TEST_LOCAL_PORT api-ui=$TEST_API_UI_PORT"

cleanup() {
  echo "▶ Stopping test containers ($COMPOSE_PROJECT_NAME)…"
  docker compose -f "$COMPOSE_FILE" down -v
}
trap cleanup EXIT

# ── Start test infrastructure ──────────────────────────────────────────────────
echo "▶ Starting isolated test containers…"
docker compose -f "$COMPOSE_FILE" up -d --build --wait

# ── Run Playwright ─────────────────────────────────────────────────────────────
# reuseExistingServer (non-CI default) means Playwright will detect the API
# already running on $TEST_API_PORT and skip launching it locally.
EXIT_CODE=0
if [[ -n "$PROJECT" ]]; then
  REUSE_API_SERVER=true PLAYWRIGHT_PROJECT="$PROJECT" pnpm exec playwright test --project="$PROJECT" || EXIT_CODE=$?
else
  REUSE_API_SERVER=true pnpm exec playwright test || EXIT_CODE=$?
fi

exit "$EXIT_CODE"
