# Glyph API

A Go REST API backend for Glyph, backed by PostgreSQL, with OIDC-based authentication.

## Stack

| Concern | Choice |
|---|---|
| Language | Go 1.22 |
| HTTP | Gin |
| Database | PostgreSQL 16 (pgx/v5) |
| Auth | Generic OIDC (JWT RS256/ES256) |
| Migrations | golang-migrate (SQL files) |

---

## Project layout

```
api/
  cmd/api/main.go            # Entry point, server wiring
  internal/
    auth/
      middleware.go          # OIDC JWT validation + user upsert middleware
      jwks.go                # JWKS fetching + caching (RSA + EC key support)
    db/
      db.go                  # pgxpool connection helper
    handler/
      handler.go             # All Gin HTTP handlers (pages, tasks, lanes, templates)
    model/
      model.go               # Domain structs (mirrors frontend types.ts)
    store/
      store.go               # Store interfaces
      user_store.go          # Postgres UserStore
      page_store.go          # Postgres PageStore + content
      task_store.go          # Postgres TaskStore
      lane_store.go          # Postgres LaneStore
      template_store.go      # Postgres TemplateStore
  migrations/
    000001_init.up.sql       # Schema: users, pages, page_contents, tasks, lanes, templates
    000001_init.down.sql     # Drop all tables
  Dockerfile                 # Multi-stage build → distroless image
  .env.example               # Environment variable reference
```

---

## Environment variables

| Variable | Description |
|---|---|
| `DATABASE_URL` | Postgres connection string, e.g. `postgres://user:pass@host:5432/db?sslmode=disable` |
| `PORT` | HTTP port (default: `8080`) |
| `OIDC_ISSUER_URL` | OIDC provider base URL, e.g. `https://accounts.google.com` |
| `OIDC_AUDIENCE` | Expected `aud` claim — your OAuth2 client ID |
| `GIN_MODE` | `debug` or `release` (default: `release`) |

Copy `.env.example` to `.env` and fill in the values for local development.

---

## Running locally

### Prerequisites

- Go 1.22+
- Docker / Docker Compose
- [golang-migrate CLI](https://github.com/golang-migrate/migrate) (optional, for running migrations outside Docker)

### With Docker Compose

```bash
# Copy and fill in OIDC values
cp api/.env.example .env

# Start Postgres, run migrations, and start the API
docker compose up postgres migrate api
```

The API will be available at `http://localhost:8080`.

### Without Docker

```bash
# Start Postgres however you prefer, then:
cd api
cp .env.example .env  # edit .env

# Run migrations
migrate -path migrations -database "$DATABASE_URL" up

# Start the API
go run ./cmd/api
```

---

## Authentication

Every request to `/api/v1/*` requires an `Authorization: Bearer <token>` header.

The token must be a valid JWT issued by your OIDC provider:

1. Your frontend performs the OIDC Authorization Code flow and receives an ID token (or access token with OIDC claims).
2. The token is passed as a Bearer token on each API call.
3. The API validates the JWT signature against the provider's JWKS endpoint (auto-discovered via `<OIDC_ISSUER_URL>/.well-known/openid-configuration`), verifies `iss`, `aud`, and `exp`, and upserts the user row in Postgres on first login.

### Provider examples

| Provider | `OIDC_ISSUER_URL` | `OIDC_AUDIENCE` |
|---|---|---|
| Auth0 | `https://<tenant>.auth0.com/` | Your Auth0 application Client ID |
| Google | `https://accounts.google.com` | Your Google OAuth2 Client ID |
| Keycloak | `https://<host>/realms/<realm>` | Your Keycloak client ID |
| Okta | `https://<tenant>.okta.com` | Your Okta application Client ID |

---

## API reference

All routes are prefixed with `/api/v1`. All request and response bodies are JSON.

### Health

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Liveness check (no auth) |

### Pages

| Method | Path | Description |
|---|---|---|
| `GET` | `/pages` | List all pages/folders for the current user |
| `POST` | `/pages` | Create a page or folder |
| `GET` | `/pages/:id` | Get a page by ID |
| `PATCH` | `/pages/:id` | Update a page |
| `DELETE` | `/pages/:id` | Delete a page (cascades to children and content) |
| `GET` | `/pages/:id/content` | Get ProseMirror JSON content for a page |
| `PUT` | `/pages/:id/content` | Save ProseMirror JSON content for a page |

### Tasks

| Method | Path | Description |
|---|---|---|
| `GET` | `/tasks` | List all tasks for the current user |
| `POST` | `/tasks` | Create a task |
| `GET` | `/tasks/:id` | Get a task by ID |
| `PATCH` | `/tasks/:id` | Update a task |
| `DELETE` | `/tasks/:id` | Delete a task |

### Lanes

| Method | Path | Description |
|---|---|---|
| `GET` | `/lanes` | List all lanes for the current user |
| `POST` | `/lanes` | Create a lane |
| `GET` | `/lanes/:id` | Get a lane by ID |
| `PATCH` | `/lanes/:id` | Update a lane |
| `DELETE` | `/lanes/:id` | Delete a lane |

### Templates

| Method | Path | Description |
|---|---|---|
| `GET` | `/templates` | List all note templates for the current user |
| `POST` | `/templates` | Create a template |
| `GET` | `/templates/:id` | Get a template by ID |
| `PATCH` | `/templates/:id` | Update a template |
| `DELETE` | `/templates/:id` | Delete a template |

---

## Running migrations manually

```bash
# Apply all pending migrations
migrate -path api/migrations -database "$DATABASE_URL" up

# Roll back one migration
migrate -path api/migrations -database "$DATABASE_URL" down 1
```
