# Glyph — Copilot Instructions

> **Keep these instructions up to date.** As the application evolves, update the relevant sections here. When a non-obvious challenge is solved (a tricky Svelte 5 rune pattern, a TipTap quirk, a ProseMirror gotcha), document the solution so future agents and contributors don't repeat the work. Treat this file as a living technical wiki for the codebase.

---

## Contributing guidelines

- Write E2E tests for all new features and bug fixes. Tests go in `e2e/` and use Playwright with the page object pattern. Make sure they pass.

---

## Project overview

Glyph is a notes-taking and task-tracking app. Key ideas:

- A **WYSIWYG markdown editor** (TipTap / ProseMirror). Typing `#` instantly becomes a heading, `-` becomes a bullet, etc.
- Bullet list items placed under a heading called **TODO** automatically trigger task creation. The bullet and the resulting task are **bidirectionally linked** via a stable `nodeId` attribute in the ProseMirror document.
- A **kanban-style task board** with user-configurable lanes. Each lane has filter rules and a sort mode (auto / field / manual). The sort system is abstracted for future AI sorting.
- A **hierarchical page tree** — pages and folders, unlimited depth.
- A **full-text search** page and `⌘K` modal, powered by Fuse.js. The search layer is abstracted for future AI/semantic search.
- **Two storage backends** — LocalStorage (offline, no setup) or Go REST API + PostgreSQL. The storage abstraction layer lets consumers use either without code changes.
- **Organizations & sharing** — Users can create orgs, invite members, and share pages/tasks/templates with org-level or direct-user permissions.

---

## Tech stack

| Concern | Choice |
|---|---|
| Framework | SvelteKit 2 + Svelte 5 (runes) |
| Language | TypeScript (strict) |
| Editor | TipTap 3 (`@tiptap/core`, `@tiptap/starter-kit`) |
| Styling | Svelte scoped `<style>` + CSS custom properties (no CSS-in-JS library) |
| Search | Fuse.js 7 (`FuseSearchProvider`) |
| IDs | `nanoid` |
| Dates | `date-fns` 4 |
| Storage | `LocalStorageAdapter` or `apiClient` → `Repository<T>` base class |
| Backend | Go (Gin), PostgreSQL 16, OIDC auth (Zitadel) |
| Package manager | pnpm |

---

## Commands

The Makefile is the single entry point for tests and linting — it mirrors all CI jobs.

```bash
make ci             # everything: lint + unit tests + both E2E projects
make test           # unit tests + both E2E projects (no lint)
make test-unit      # fast: frontend vitest + Go unit tests (no e2e)
make test-frontend  # pnpm check + pnpm build + pnpm test
make test-go        # go vet + go build + go test -short
make test-e2e-local # Playwright local-storage project (no backend needed)
make test-e2e-api   # Playwright API project via isolated Docker stack
make lint           # svelte-check + golangci-lint + go vet
```

Direct pnpm / go commands (for quick iteration during development):

```bash
pnpm dev          # start dev server at http://localhost:5173
pnpm build        # production build (also used to catch type/lint errors)
pnpm check        # svelte-check + tsc type checking
pnpm preview      # preview production build
```

After making changes, always run `pnpm build` (or `pnpm check`) to verify there are no TypeScript or Svelte compiler errors before considering a task done. For Go changes, run `cd api && go build ./...` and `go test ./... -short`.

---

## Project structure

```
src/
  app.css                          # Global CSS custom properties + utility classes
  app.html                         # HTML shell (sets data-theme="dark")
  lib/
    models/
      types.ts                     # ALL TypeScript interfaces live here — single source of truth
    storage/
      LocalStorageAdapter.ts       # Implements StorageAdapter (sync, localStorage)
      apiClient.ts                 # REST client for Go API backend
      config.ts                    # Storage mode detection (localStorage vs API)
      Repository.ts                # Generic async-ready base: getAll/getById/create/update/delete/upsert
      repositories/
        PageRepository.ts          # Extends Repository<TreeNode>; also manages PageContent separately
        TaskRepository.ts          # Extends Repository<Task>; has applyFilter()
        LaneRepository.ts          # Extends Repository<Lane>; has getOrdered()
    stores/
      auth.svelte.ts               # Svelte 5 $state store — OIDC auth state, user info
      orgs.svelte.ts               # Svelte 5 $state store — organization CRUD, membership
      pages.svelte.ts              # Svelte 5 $state store — page tree CRUD + content save/load
      tasks.svelte.ts              # Svelte 5 $state store — task CRUD + filter
      lanes.svelte.ts              # Svelte 5 $state store — lane CRUD, seeds 4 default lanes (All Tasks, In Progress, Done, Cancelled) when storage is empty
      templates.svelte.ts          # Svelte 5 $state store — note template CRUD
      notifications.svelte.ts      # Svelte 5 $state store — toast notifications
      ui.svelte.ts                 # currentPageId, sidebarOpen, searchOpen, pendingTaskCreation
    editor/
      extensions/
        TaskLinkExtension.ts       # Extends ListItem with nodeId + taskId attrs; setTaskIdForNode command
        TodoDetectionExtension.ts  # ProseMirror plugin — detects unlinked bullets under TODO headings
    sort/
      AutoSortProvider.ts          # priority weight → dueDate asc (sync, isAsync: false)
      FieldSortProvider.ts         # generic field + direction (sync, isAsync: false)
    search/
      FuseSearchProvider.ts        # Fuse.js wrapper (sync, isAsync: false)
    components/
      editor/
        Editor.svelte              # Mounts TipTap; handles hover preview state; debounced save
        TaskCreationPopover.svelte # Modal shown when a new TODO bullet is detected (title, priority, due date, tags, and URL/link)
        TaskHoverPreview.svelte    # Floating tooltip anchored near the linked bullet (stays open while hovered so "Open" is clickable)
      sidebar/
        Sidebar.svelte             # App name, nav links, section header, new page/folder buttons
        PageTree.svelte            # Renders children of a given parentId
        TreeNodeItem.svelte        # Recursive single node row with context menu
      tasks/
        Lane.svelte                # Single lane column — filters + sorts tasks, skeleton while loading
        LaneConfig.svelte          # Modal to configure lane title, filters, sort
        TaskCard.svelte            # Card in a lane — status dot, title, priority badge, due, tags
      search/
        SearchModal.svelte         # ⌘K overlay, inline search
      shared/
        TagInput.svelte            # Reusable tag pill input with autocomplete
  routes/
    +layout.svelte                 # App shell: loads all stores, sidebar, ⌘K / ⌘N handlers
    +page.svelte                   # Root — redirects to first page on first load
    notes/[pageId]/+page.svelte    # Notes page with Editor
    tasks/+page.svelte             # Task board (LaneBoard)
    tasks/[taskId]/+page.svelte    # Task detail — all fields editable inline
    search/+page.svelte            # Full search page
    settings/orgs/+page.svelte     # Organization management (API mode only)
```

---

## Key architectural patterns

### Svelte 5 stores (`.svelte.ts`)

All stores use the Svelte 5 **runes** API inside a plain factory function. They are NOT Svelte components and have no `$effect` at the module level — state is only read/written in functions. This is intentional to avoid SSR issues.

```ts
// Correct pattern
function createMyStore() {
  let items = $state<Item[]>([]);

  async function load() { items = await repo.getAll(); }
  async function add(item: Item) { await repo.create(item); items = [...items, item]; }

  return {
    get items() { return items; },
    load,
    add
  };
}
export const myStore = createMyStore();
```

Always use a getter (`get items()`) to expose `$state` values — do NOT return the raw variable, or reactivity won't propagate through object destructuring.

### Storage abstraction

All data access goes through a `Repository<T>` subclass, which returns `Promise<T>` for every operation. The `LocalStorageAdapter` is injected at instantiation and is the only sync boundary. Consumers (stores) always `await` repository calls.

To swap to an async backend (IndexedDB, REST, Supabase): implement `StorageAdapter` and pass it to the repositories. No store or component code changes.

### Sort abstraction

```ts
export interface SortProvider<T> {
  isAsync: boolean;
  sort(items: T[], config: SortConfig): Promise<T[]>;
}
```

`Lane.svelte` checks `provider.isAsync` before calling `sort()`. When `true`, it sets `loading = true` and renders skeleton cards. This is the seam for a future AI sort provider — just set `isAsync: true` and return a `Promise` that resolves after the model responds.

Currently available providers:
- `AutoSortProvider` — priority weight then dueDate ASC (sync)
- `FieldSortProvider` — any `keyof Task`, asc or desc (sync)
- Manual mode — stored as `lane.sortConfig.taskOrder: string[]`, sorted client-side without a provider

### Search abstraction

```ts
export interface SearchProvider {
  isAsync: boolean;
  index(items: SearchableItem[]): Promise<void>;
  search(query: string, opts?: SearchOptions): Promise<SearchResult[]>;
}
```

The search index is rebuilt (debounced) on every store mutation via a `$effect` in `search/+page.svelte`. When swapping to an AI provider, set `isAsync: true` and show a spinner while the `Promise` resolves.

### TODO-bullet → Task pipeline

1. User types a bullet under any heading whose `.textContent` matches `/^todo$/i`.
2. `TodoDetectionExtension` fires an `appendTransaction` ProseMirror plugin on every doc change. It scans `doc.descendants()` for `listItem` nodes with a `nodeId` but no `taskId`.
3. For each such node it calls `options.onTodoBulletCreated(...)` — an async callback passed in from `Editor.svelte`.
4. `Editor.svelte` stores a `PendingTaskCreation` in local state and renders `TaskCreationPopover`.
5. On confirm, it calls `editor.commands.setTaskIdForNode(nodeId, taskId)` (added by `TaskLinkExtension`) to write the `taskId` attribute into the ProseMirror document. The document is then saved.
6. `promptedNodeIds: Set<string>` prevents re-prompting for the same `nodeId` on subsequent keystrokes.

**Critical:** `nodeId` is a stable UUID assigned once when a `listItem` node is created and never changes. ProseMirror positions are ephemeral — never store a position as a link. Always use `nodeId`.

### TipTap / ProseMirror notes

- **`SvelteNodeViewRenderer` (from `svelte-tiptap`) is intentionally avoided.** It has unresolved Svelte 5 rune mutation bugs (open issues #76, #80, #81 as of April 2026). Custom in-editor UI uses vanilla ProseMirror `NodeView` via `addNodeView()` in an extension, or coordinate-positioned Svelte portals (like `TaskHoverPreview`).
- `svelte-tiptap` is only used for `FloatingMenu`/`BubbleMenu` components, which are not affected by the above bugs.
- `StarterKit` is configured with `listItem: false` to allow `TaskLinkExtension` (which extends `ListItem`) to replace it. If `listItem` is not disabled in StarterKit, you get a schema conflict at runtime.
- TipTap input rules for headings and lists fire synchronously in `appendTransaction`. Any code that reads `editor.state.doc` from inside `onUpdate` is reading the **post-transaction** state, not mid-transaction.

### CSS / theming

There is no CSS-in-JS. All styles live in:
- `src/app.css` — global reset, CSS custom properties (the full Obsidian dark palette), utility classes (`.badge-*`, `.tag-pill`, `.skeleton`, `.btn-primary`, `.btn-ghost`, etc.)
- Per-component `<style>` blocks (scoped)

**Never hardcode colors** — always use a CSS variable from the palette. Add new tokens to `app.css` first.

Key tokens:
```
--bg-primary / --bg-secondary / --bg-tertiary / --bg-hover / --bg-modal
--border-subtle / --border-default / --border-strong
--text-primary / --text-secondary / --text-muted / --text-heading
--accent / --accent-hover / --accent-muted / --accent-bg
--priority-urgent/high/medium/low/none
--status-todo/in-progress/done/cancelled
```

### Editor heading margin snap fix

When a paragraph is converted to a heading by a TipTap input rule, the heading's `margin-top` suddenly applies, causing the cursor to appear to jump down. This is fixed by:

```css
:global(.tiptap-editor > :first-child) { margin-top: 0 !important; }
```

This rule is in `Editor.svelte`. Do not remove it.

---

## Type system

All interfaces are in `src/lib/models/types.ts`. Key types:

| Type | Purpose |
|---|---|
| `TreeNode` | A page or folder in the hierarchy (`type: 'page' \| 'folder'`) |
| `PageContent` | Stores the serialized ProseMirror JSON for a page (separate from `TreeNode`) |
| `Task` | A task with status, priority, dueDate, tags, description, and `sourceNodeId` / `sourcePageId` |
| `Lane` | A kanban column with a `FilterSet`, `SortConfig`, and optional `taskOrder` |
| `FilterSet` | `{ conjunction: 'and' \| 'or', rules: FilterRule[] }` |
| `FilterRule` | `{ field: keyof Task, operator: FilterOperator, value: unknown }` |
| `SortConfig` | `{ mode: 'auto' \| 'field' \| 'manual', field?, direction?, taskOrder? }` |
| `NoteTemplate` | A reusable note template with title, content, tags, and optional default folder |
| `Organization` | An org with id, name, ownerId |
| `OrgMember` | A member in an org with userId, email, role |
| `Share` | Direct user share on a resource (page/task/template) with permission level |
| `PendingTaskCreation` | Transient UI state: `{ nodeId, bulletText, pageId, resolve }` |
| `SortProvider<T>` | Interface with `isAsync: boolean` and `sort(): Promise<T[]>` |
| `SearchProvider` | Interface with `isAsync: boolean`, `index()`, and `search()` |

---

## Routing

| Route | File | Purpose |
|---|---|---|
| `/` | `routes/+page.svelte` | Redirects to first page (handled in layout `onMount`) |
| `/notes/[pageId]` | `routes/notes/[pageId]/+page.svelte` | Note editor |
| `/tasks` | `routes/tasks/+page.svelte` | Kanban board |
| `/tasks/[taskId]` | `routes/tasks/[taskId]/+page.svelte` | Task detail |
| `/search` | `routes/search/+page.svelte` | Full search page |
| `/settings/orgs` | `routes/settings/orgs/+page.svelte` | Organization management (API mode only) |

The layout (`+layout.svelte`) loads all three stores in parallel on `onMount`, then redirects from `/` to the first page (or creates a "Getting Started" page if none exist).

---

## Keyboard shortcuts

| Shortcut | Action |
|---|---|
| `⌘K` / `Ctrl+K` | Open search modal |
| `⌘N` / `Ctrl+N` | Create new page (only when focus is not in an input/textarea) |

Both are registered in `+layout.svelte`'s `handleKeydown` function on `<svelte:window>`.

---

## LocalStorage key schema

All keys are prefixed with `glyph:`.

| Key pattern | Content |
|---|---|
| `glyph:pages` | `TreeNode[]` |
| `glyph:tasks` | `Task[]` |
| `glyph:lanes` | `Lane[]` |
| `glyph:content:{pageId}` | `PageContent` (ProseMirror JSON + updatedAt) |

Page content is stored separately from the tree to keep the tree reads lightweight (the tree is loaded on every app start; content is loaded only when a page is opened).

---

## Known limitations & future work

### Planned but not yet built
- AI sort provider (seam: `SortProvider<Task>` with `isAsync: true`)
- AI / semantic search provider (seam: `SearchProvider` with `isAsync: true`)
- "Open source note" scroll-to-node (navigate to `/notes/[pageId]` and scan for `nodeId` in the doc)

### Known issues / gotchas to document as they are solved

- TipTap 3 `appendTransaction` fires for every transaction including selection changes. Keep detection logic cheap — avoid full-doc traversals on selection-only transactions by checking `transactions.some(tr => tr.docChanged)` first. (`TodoDetectionExtension` already does this.)
- `$effect` in `.svelte.ts` store files does **not** run during SSR. Stores call `load()` from `onMount` in the layout to avoid SSR issues.
- `LaneConfig` intentionally initializes its local `$state` from `lane` prop at mount time (not reactively). This is correct — the modal is destroyed and recreated each time it opens for a different lane. The `const init = lane` pattern suppresses the Svelte 5 `state_referenced_locally` warning while making the intent explicit.
- The `@tiptap/extension-list-item` package must be installed separately even though `@tiptap/starter-kit` includes list items internally. `TaskLinkExtension` extends the exported `ListItem` class directly.
- **`TaskHoverPreview` must not close on `mouseenter`.** The preview floats *above* the bullet, so reaching its "Open" link means the pointer leaves the bullet and travels over other editor content. `Editor.svelte` therefore uses a **deferred close** (`scheduleHoverClose`, ~180 ms) on `mouseout`/non-bullet `mouseover`, and the preview's own `onenter`/`onleave` callbacks (`cancelHoverClose` / `clearHoverPreview`) keep it open while hovered. Never wire the preview's `onmouseenter` to `onclose` — that regresses the "Open" button (it hides before it can be clicked).
- `TaskCreationPopover` has a **URL/link field** mirroring the task-detail page. It is gated by `canUnfurl` (`storageMode === 'api'`) OR an existing `task.link`, so the input only appears in API mode (the `/api/v1/unfurl` endpoint is API-only). The link is persisted immediately via `tasksStore.updateTask(taskId, { link })` (not through the debounced priority/dueDate/tags persist effect). A saved link renders via the shared `LinkPreview` component with a remove button.

---

## Local development with Docker Compose

Two compose profiles are available:

| Command | What runs |
|---|---|
| `docker compose up` | Vite dev server only (localStorage mode) |
| `docker compose -f docker-compose.yml -f docker-compose.postgres.yml up` | Vite + Go API + Postgres + migrations |

### Migrations

Migrations are SQL files in `api/migrations/` managed by [golang-migrate](https://github.com/golang-migrate/migrate).

**They run automatically** — the `migrate` Docker Compose service runs once at startup and exits before the `api` service starts (`service_completed_successfully` dependency). You never need to run migrations manually in local dev; just (re)start the stack.

To add a migration:
1. Create `api/migrations/NNNNNN_<description>.up.sql` and `…down.sql` (next sequential number).
2. Restart the stack — `migrate` will pick them up automatically.

To roll back one step manually:
```bash
migrate -path api/migrations -database "$DATABASE_URL" down 1
```

### Resetting local state

| Backend | How to reset |
|---|---|
| localStorage | Clear browser storage (DevTools → Application → Local Storage → Clear all) |
| Postgres | `docker compose … down -v` (drops the `postgres_data` volume) then `up` again |

After a Postgres reset, the `migrate` service re-runs all migrations on next `up`.

---

## Playwright E2E — isolated test containers

The API tests require Postgres + the Go API. To avoid conflicting with a running dev stack, use `docker-compose.test.yml`, which runs in a **separate Docker project** (`glyph-test`) on different ports:

| Service | Dev port | Test port |
|---|---|---|
| Postgres | 5432 | 5433 |
| Go API | container-internal | 8083 |

The test Postgres is **ephemeral** (no named volume) — every `up` starts with a clean database.

### Automated (preferred)

```bash
pnpm test:e2e:api:docker   # start containers → run api tests → tear down
pnpm test:e2e:docker       # same but for all projects
```

### Manual

```bash
# Start the isolated stack and wait for healthy
docker compose -f docker-compose.test.yml up -d --build --wait

# Run tests (Playwright sees the API already on :8083 and reuses it)
pnpm test:e2e:api

# Tear down and delete the ephemeral volume
docker compose -f docker-compose.test.yml down -v
```

### How it works with `playwright.config.ts`

`reuseExistingServer: true` (the non-CI default) means Playwright will detect the API already listening on `:8083` and skip running `go run ./cmd/api` locally. The SvelteKit dev server still starts on `:5174`, proxying to the Docker API.

---

## CI/CD

Glyph deploys via GitHub Actions on push to `main`, using a self-hosted runner on the k3s homelab cluster.

### How it works

- **Runner**: ARC (Actions Runner Controller) v2, deployed in the `github-runner` namespace of the homelab cluster. Runners scale 0→3 on demand.
- **Workflow**: `.github/workflows/deploy.yml` — builds Docker images, pushes to `registry.kurpuis.com:5000`, runs `helm upgrade` into the `glyph` namespace.
- **`runs-on: arc-runner-set`** — must match the Helm release name of the runner scale set. Do NOT use `self-hosted`.
- **Docker**: available via DinD (Docker-in-Docker) sidecar in the runner pod. No extra setup needed for `docker build`/`docker push`.
- **Helm & kubectl**: not pre-installed in the runner image. The workflow uses `azure/setup-helm@v4` and `azure/setup-kubectl@v4` to install them.
- **Registry auth**: stored as GitHub Actions **repository secrets** (`REGISTRY_USERNAME`, `REGISTRY_PASSWORD`). The workflow runs `docker login` before building.
- **Cluster auth**: the runner pod uses the `ci-deployer` ServiceAccount (bound via RoleBinding in the `glyph` namespace), which has permissions for Helm releases, deployments, services, jobs, etc. No kubeconfig needed.

### Infrastructure location

The runner infrastructure lives in the **homelab** repo, not in glyph:
- RBAC manifests: `homelab/kubernetes/manifests/github-runner/`
- Helm values: `homelab/kubernetes/helm/deployments/github-runner/`

See `homelab/kubernetes/manifests/github-runner/README.md` for full setup and troubleshooting.
