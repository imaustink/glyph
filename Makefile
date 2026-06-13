# Glyph — development task runner
#
# Targets mirror the CI jobs in .github/workflows/ci.yml so that
# `make test` and `make lint` are the canonical local equivalents.
#
# Quick reference:
#   make test              – all tests (unit + e2e)
#   make test-unit         – fast: frontend unit + Go unit (no e2e)
#   make test-frontend     – type-check, build, vitest
#   make test-go           – vet, build, go test
#   make test-e2e-local    – Playwright local-storage project
#   make test-e2e-api      – Playwright API project (Docker stack)
#   make lint              – all linters
#   make lint-frontend     – svelte-check + tsc
#   make lint-go           – go vet + golangci-lint

.DEFAULT_GOAL := help

# ── Helpers ───────────────────────────────────────────────────────────────────

GOLANGCI_LINT := $(shell command -v golangci-lint 2>/dev/null || echo $(shell go env GOPATH)/bin/golangci-lint)

.PHONY: _require-golangci-lint
_require-golangci-lint:
	@test -x "$(GOLANGCI_LINT)" || { \
		echo "golangci-lint not found. Install it with:"; \
		echo "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b \$$(go env GOPATH)/bin latest"; \
		exit 1; \
	}

# ── Frontend ──────────────────────────────────────────────────────────────────

## lint-frontend: Type-check and svelte-check (mirrors CI "Type check" step)
.PHONY: lint-frontend
lint-frontend:
	pnpm check

## test-frontend: Type-check, build, and run vitest unit tests
.PHONY: test-frontend
test-frontend: lint-frontend
	pnpm build
	pnpm test

# ── Go API ────────────────────────────────────────────────────────────────────

## lint-go: go vet + golangci-lint (mirrors CI "Vet" and "Lint" steps)
.PHONY: lint-go
lint-go: _require-golangci-lint
	cd api && go vet ./...
	cd api && $(GOLANGCI_LINT) run ./...

## test-go: vet, build, and run Go unit tests with -short (mirrors CI "api" job)
.PHONY: test-go
test-go:
	cd api && go vet ./...
	cd api && go build ./...
	cd api && go test ./... -short -count=1

# ── E2E ───────────────────────────────────────────────────────────────────────

## test-e2e-local: Playwright local-storage project (no backend; dev server auto-started)
.PHONY: test-e2e-local
test-e2e-local:
	pnpm test:e2e:local

## test-e2e-api: Playwright API project via isolated Docker stack (mirrors CI "e2e-api" job)
.PHONY: test-e2e-api
test-e2e-api:
	scripts/test-e2e.sh api

## test-e2e: Both Playwright projects via Docker (local + api)
.PHONY: test-e2e
test-e2e:
	scripts/test-e2e.sh

# ── Combined ──────────────────────────────────────────────────────────────────

## lint: All linters (frontend + Go)
.PHONY: lint
lint: lint-frontend lint-go

## test-unit: Fast unit tests only — frontend vitest + Go unit tests (no e2e)
.PHONY: test-unit
test-unit: test-frontend test-go

## test: Full test suite — unit tests + both E2E projects (matches all CI jobs)
.PHONY: test
test: test-frontend test-go test-e2e

## ci: Everything — lint + full test suite (the single command to rule them all)
.PHONY: ci
ci: lint-go test-frontend test-go test-e2e

# ── Help ──────────────────────────────────────────────────────────────────────

## help: Show this help message
.PHONY: help
help:
	@echo ""
	@echo "Usage: make <target>"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) \
		| sed 's/^## //' \
		| column -t -s ':'
	@echo ""
