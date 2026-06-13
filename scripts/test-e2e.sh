#!/usr/bin/env bash
# scripts/test-e2e.sh — Run Playwright E2E tests against an isolated Docker stack.
#
# Starts the glyph-test compose project (Postgres on :5433, Go API on :8083),
# waits until both are healthy, runs the requested Playwright project(s), then
# tears everything down — even on failure.
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

# ── Start test infrastructure ──────────────────────────────────────────────────
echo "▶ Starting isolated test containers…"
docker compose -f "$COMPOSE_FILE" up -d --build --wait

# ── Run Playwright ─────────────────────────────────────────────────────────────
# reuseExistingServer (non-CI default) means Playwright will detect the API
# already running on :8083 and skip launching it locally.
EXIT_CODE=0
if [[ -n "$PROJECT" ]]; then
  REUSE_API_SERVER=true PLAYWRIGHT_PROJECT="$PROJECT" pnpm exec playwright test --project="$PROJECT" || EXIT_CODE=$?
else
  REUSE_API_SERVER=true pnpm exec playwright test || EXIT_CODE=$?
fi

# ── Tear down (always) ─────────────────────────────────────────────────────────
echo "▶ Stopping test containers…"
docker compose -f "$COMPOSE_FILE" down -v

exit "$EXIT_CODE"
