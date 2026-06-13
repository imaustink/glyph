# Glyph

A notes-taking and task-tracking app with a WYSIWYG markdown editor and kanban-style task board.

> **Design philosophy:** Task management should never be separate from the thinking that produces it. Glyph treats your notes as the source of truth — write a bullet under a TODO heading and it becomes a trackable task automatically. No context switching, no copy-pasting into a separate tool. If you can write a bullet list, you can run a project.

- **Rich editor** — TipTap / ProseMirror. Typing `#` becomes a heading, `-` becomes a bullet, etc.
- **TODO detection** — Bullets under a `TODO` heading automatically create linked tasks.
- **Kanban board** — Configurable lanes with filters and sorting (auto, field, or manual).
- **Page tree** — Hierarchical pages and folders, unlimited depth.
- **Search** — Full-text search via `⌘K` modal or dedicated search page (Fuse.js).
- **Two storage backends** — LocalStorage (offline, no setup) or Go REST API + PostgreSQL.

## Tech stack

| Layer | Choice |
|---|---|
| Frontend | SvelteKit 2, Svelte 5 (runes), TypeScript |
| Editor | TipTap 3 (`@tiptap/core`, `@tiptap/starter-kit`) |
| Styling | Scoped `<style>` + CSS custom properties |
| Search | Fuse.js 7 |
| Backend | Go (Gin), PostgreSQL 16, OIDC auth |
| Package manager | pnpm |

## Prerequisites

- Node.js 20+
- pnpm 9+
- Docker (for Postgres backend)
- Go 1.22+ (for API backend)

## Getting started

### Local mode (no backend)

```bash
pnpm install
pnpm dev              # http://localhost:5173 (localStorage mode)
```

### Docker Compose

Docker Compose is used for local development only. All services run with live reload by default.

```bash
# localStorage mode — Vite dev server on http://localhost:5173
docker compose up

# Full stack (API + Postgres) — Vite on http://localhost:5173, Go API rebuilt on save via Air
docker compose -f docker-compose.yml -f docker-compose.postgres.yml up
```

In full-stack mode, the app is also available at `https://localhost` (self-signed cert via Nginx). **Migrations run automatically** — the `migrate` service runs once before the API starts and then exits; no manual steps required.

### Resetting local state

| Backend | How to reset |
|---|---|
| localStorage | Clear browser storage (DevTools → Application → Local Storage → Clear all) |
| Postgres | `docker compose … down -v` then `up` again (drops the volume; migrations re-run on next start) |

## Scripts

```bash
pnpm dev          # start dev server (localStorage mode) at http://localhost:5173
pnpm build        # production build (also catches type errors)
pnpm check        # svelte-check + tsc type checking
pnpm preview      # preview production build
```

## Testing

The Makefile is the single entry point for running tests and linters — it mirrors all CI jobs exactly.

```bash
make ci             # everything: lint + unit tests + both E2E projects
make test           # unit tests + both E2E projects (no lint)
make test-unit      # fast: frontend vitest + Go unit tests only
make test-frontend  # type-check, build, vitest
make test-go        # go vet, build, unit tests
make test-e2e-local # Playwright local-storage project (no backend needed)
make test-e2e-api   # Playwright API project via isolated Docker stack
make lint           # all linters (svelte-check + golangci-lint + go vet)
```

Run `make help` for the full reference.

### Go unit tests only

```bash
cd api && go test ./... -short
```

Runs unit tests (handler, model, store, memstore packages). Pass `-short` to skip the integration suite that requires Postgres.

### Playwright E2E — API backend

The `make test-e2e-api` target (and `make ci`) uses an isolated Docker stack to avoid conflicting with a running dev environment:

```bash
docker compose -f docker-compose.test.yml up -d --build --wait
pnpm test:e2e:api
docker compose -f docker-compose.test.yml down -v
```

This starts an ephemeral Postgres (port 5433) and Go API (port 8083) in their own Docker project (`glyph-test`), then tears everything down after the run.

## Project structure

```
src/
  lib/
    components/        # Svelte components (editor, sidebar, tasks, search, shared)
    editor/extensions/ # TipTap extensions (TaskLink, TodoDetection)
    models/types.ts    # All TypeScript interfaces
    search/            # Search provider abstraction (Fuse.js)
    sort/              # Sort provider abstraction (Auto, Field)
    storage/           # Storage layer (Repository, LocalStorage, API client)
    stores/            # Svelte 5 rune stores (pages, tasks, lanes, ui)
  routes/              # SvelteKit routes (notes, tasks, search)
api/
  cmd/api/main.go      # Go API entry point
  internal/            # Auth, handlers, models, stores
  migrations/          # PostgreSQL schema migrations
e2e/                   # Playwright E2E test specs
```

See [api/README.md](api/README.md) for API-specific documentation.
