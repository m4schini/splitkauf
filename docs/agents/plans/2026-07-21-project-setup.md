---
date: 2026-07-21T16:40:49+00:00
git_commit: ee412e7abf80ac069e9c09caa63cdf430ef042ef
branch: main
topic: "Project setup: Go backend + React PWA frontend + dev harness + CI/deployment"
tags: [plan, setup, go, react, postgres, pwa, harness, ci, quadlet]
status: complete
---

# PLAN: Splitkauf Project Setup

Bootstrap the splitkauf repository into a working full-stack skeleton:

- Go service at the repository root, following the **community-template** architecture (hexagonal layout, spec-first REST via oapi-codegen, cobra/viper/zap/prometheus) but with **no ais-specific dependencies** (no `ais-go`, no `ais-schule` module path, no ais reusable CI workflows, no helm chart, no `hack/template.sh`).
- React (Vite + TypeScript, npm) frontend in `frontend/`, built as a basic PWA shell and embedded into the Go binary via `go:embed`.
- PostgreSQL wiring: pgx v5 (stdlib driver), golang-migrate with embedded migrations, `migrate` cobra subcommand — modeled on `community-exchange/database`.
- Development harness: pre-commit hooks, Claude Code hooks, `make check` aggregate, golangci-lint, frontend lint/format/typecheck/test — automated backpressure for AI-assisted development.
- GitHub Actions CI, renovate.json, multi-stage Dockerfile, docker-compose, and podman quadlet systemd units for deployment.

Reference sources (read-only, outside this repo):

- `/Users/malte.schink/Projects/AIS/community-template` — architecture, Makefile, Dockerfile, config/telemetry/ports packages, pre-commit, workflows
- `/Users/malte.schink/Projects/AIS/community-exchange` — `database/migrations.go`, `cmd/migrate.go`, `adapters/db/db.go`, `config.DatabaseConfig.DSN()`

## Acceptance Criteria

- `make build` compiles all packages from a clean checkout (frontend stub embedded); `make dist` produces the single release binary `./splitkauf` embedding `splitkauf.openapi.yaml` **and** the built React frontend; `make test`, `make lint`, `make fmt-check`, `make tidy-check`, `make security` all pass.
- `docker compose up -d postgres` + `go run . migrate` applies the embedded baseline migration; `go run . serve` starts the API; `GET /api/v1/health` returns OK including DB reachability.
- Frontend dev loop: `npm run dev` in `frontend/` proxies `/api` to the Go server on :8080; the production build is served by the Go binary with SPA fallback; the app is installable as a basic PWA (manifest, icons, iOS meta tags, precache-only service worker).
- Dev harness gives backpressure: `pre-commit run --all-files` passes (hygiene + gitleaks + Go fmt/lint + frontend lint/format + conventional-commit check); Claude Code `PostToolUse` hooks auto-format/lint edited files; a single `make check` runs every local gate; GitHub Actions mirrors the same checks.
- Podman quadlet unit files for app + postgres exist under `deploy/quadlet/`; `renovate.json` present; case-sensitive `grep -r "TEMPLATE\|ais-schule" --exclude-dir={docs,.git,node_modules,dist}` over the repo finds nothing.

## Technical Key Decisions and Tradeoffs

1. **Copy community-template architecture, not its repo-specific glue:** hexagonal layout (`app/`, `ports/rest/`, `adapters/`, `cmd/`, `config/`, `telemetry/`), spec-first `splitkauf.openapi.yaml`, oapi-codegen chi-server + typed client, cobra/viper/zap/prometheus.
   - Why: proven, documented in `docs/research/go-backend-community-template.md`; all its Go dependencies are generic.
   - Impact: files are hand-copied and adapted (module path, ServiceName), not scaffolded via `hack/template.sh`. Dropped entirely: `hack/`, `helm/`, ais reusable workflows, coverage-comment CI machinery.
2. **Module path `github.com/m4schini/splitkauf`:** env prefix `SPLITKAUF_`, spec file `splitkauf.openapi.yaml`, image name `splitkauf`.
3. **HTTP server behind an explicit `serve` subcommand**; the root command only prints help.
   - Why: resolves the template's no-op root ambiguity; symmetric with the `migrate` subcommand.
   - Impact: `go run . serve` locally; quadlet/compose run `["serve"]` as the container command (Dockerfile `ENTRYPOINT ["/app"]`, `CMD ["serve"]`).
4. **Postgres via pgx v5 stdlib driver + `database/sql`; migrations via golang-migrate + `iofs` embed**, adapted from `community-exchange/database` (`Migrate`/`MigrateDown`/`OverrideDirty`, `migrate --version/--force/--dangerously-destroy-database`). No ORM (exchange's xorm is deliberately not adopted; the collaborative-lists research recommends plain parameterized SQL).
   - Impact: `database/` holds migration engine + embedded SQL files; `adapters/db/` holds the connection constructor. Baseline migration `000001_init` only enables `pgcrypto` — the domain schema comes with the data-model plan.
5. **Frontend: npm + Vite + React + TS in `frontend/`, `go:embed` into the binary.** Vite `build.outDir` is `../ports/web/dist`; `ports/web` serves the embedded FS with SPA fallback. `ports/web/dist/` is gitignored; a Makefile stub rule (`make ports/web/dist/index.html`) creates a placeholder `index.html` so the Go packages compile without a frontend build (deviation from the template's "all generated files committed" — only oapi-codegen output is committed). Caveat: tooling invoked *outside* make (bare `go build ./...`, IDE, pre-commit golangci-lint) needs the stub to exist — the pre-commit Go hooks therefore run the stub rule first.
   - Why: single artifact, fits the distroless Dockerfile and a single quadlet container.
6. **PWA shell now, offline data later:** vite-plugin-pwa (`registerType: 'autoUpdate'`, precache app shell only), manifest + iOS meta tags + apple-touch-icon exactly per `docs/agents/research/2026-07-21-pwa-ios-support.md`. No Dexie/Workbox background-sync yet.
7. **Harness = layered backpressure:** (a) Claude Code `PostToolUse` hook formats+lints each edited file immediately; (b) pre-commit blocks bad commits (hygiene, gitleaks, gofumpt, golangci-lint, eslint/prettier, conventional-commit message check per AGENTS.md — types restricted to `feat|fix|chore`); (c) `make check` as the one-command local gate; (d) CI re-runs the same gates.
8. **Deployment via podman quadlets, not helm/k8s:** `deploy/quadlet/` with `.network`, `.volume`, and two `.container` units; config via env file on the server.
9. **CI without ais-schule/actions:** plain workflows — backend (generate-clean check, build, test-unit), frontend (lint, typecheck, test, build), lint (fmt/lint/tidy/security), docker build. The template's PR coverage-comment machinery is dropped.

## Current State

```
splitkauf/                        (git repo, 1 commit, everything else untracked)
├── AGENTS.md / CLAUDE.md         # agent contribution rules (conventional commits: feat|fix|chore)
├── .claude/settings.local.json
├── docs/research/                # 5 research docs
└── docs/agents/research/         # 4 research docs (incl. this plan's sources)
```

No application code, no go.mod, no frontend, no Makefile, no CI.

## Desired End State

```
splitkauf/
├── main.go                        # embed splitkauf.openapi.yaml; cmd.Execute()
├── splitkauf.openapi.yaml         # source of truth; /health endpoint
├── go.mod                         # module github.com/m4schini/splitkauf; tool oapi-codegen
├── Makefile                       # generate/build/test/lint/fmt/tidy/security/check + frontend targets
├── Dockerfile                     # node build stage → go builder → distroless/static
├── docker-compose.yaml            # app + postgres
├── renovate.json
├── .pre-commit-config.yaml
├── .golangci.yml
├── .claude/settings.json          # PostToolUse format/lint hooks (committed)
├── .github/workflows/{ci,lint}.yml
├── deploy/quadlet/                # splitkauf.network, splitkauf-db.volume, *.container
├── hack/format-file.sh            # Claude hook target + shared formatter script
├── cmd/{root,serve,migrate}.go
├── config/{config,defaults,validation}.go + config.yaml
├── telemetry/log.go + telemetry/metrics/{metrics,middleware,server}.go
├── ports/rest/{server,api-catalog,docs}.go + middleware/logging.go
│         └── v1/{config.yaml,api.go,gen.go}   # gen.go committed
├── ports/web/web.go               # go:embed all:dist; SPA fallback (dist/ gitignored)
├── client/{config.yaml,gen.go,client.gen.go}  # client.gen.go committed
├── adapters/db/db.go              # NewSQL via pgx stdlib
├── database/migrations.go         # golang-migrate engine (iofs embed)
│         └── migrations/000001_init.{up,down}.sql
├── app/                           # empty (domain logic comes with feature plans)
└── frontend/                      # Vite + React + TS + vite-plugin-pwa (npm)
    ├── vite.config.ts             # proxy /api → :8080; outDir ../ports/web/dist
    ├── index.html                 # iOS PWA meta tags
    ├── public/icons/              # 192/512/512-maskable/apple-touch-180 PNGs
    └── src/                       # minimal app: fetches /api/v1/health, shows status
```

Request flow (identical to template, plus web + db):

```
GET /api/v1/* → chi → metrics.Middleware → middleware.Logging → gen.go wrapper → V1 method → (app/) → adapters/db
GET /*        → ports/web embedded SPA (fallback to index.html)
GET :9090/metrics → separate Prometheus server (when enabled)
```

## Abstractions and Code Reuse

Everything in `cmd/`, `config/`, `telemetry/`, `ports/rest/` is a direct adaptation of the community-template files (same structure, renamed identifiers, `ServiceName = "splitkauf"`). `database/` and `adapters/db/` + `cmd/migrate.go` are adaptations of community-exchange, with `github.com/ais-schule/ais-go/telemetry` replaced by the local `telemetry` package and the xorm parts (`NewEngine`, `logger.go`) omitted. New abstractions introduced by this plan:

- `ports/web` — `Handler() http.Handler` serving the embedded SPA with index-fallback; only consumer is `ports/rest/server.go`.
- `hack/format-file.sh` — single formatter entry point shared by the Claude hook and usable manually.

## Logging & Observability

Same model as the template: `telemetry.Logger(names...)` (named zap, init-once, level/mode from config) is the only logging entry point; RED metrics in a custom Prometheus registry served on a separate `:9090` server when `metrics.enabled=true`; chi-route-pattern labels. Migration logging mirrors community-exchange:

```
INFO  splitkauf.database.migrate  syncing database schema
INFO  splitkauf.database.migrate  migrated database schema  {"version": 1, "dirty": false}
INFO  splitkauf.rest               request                   {"method":"GET","path":"/api/v1/health","status":200,"duration":"1.2ms"}
```

## Implementation

### Phase 1: Go Backend Skeleton

Dependencies: None

Compiling, tested, lint-clean Go service with a spec-first `/health` endpoint, serve subcommand, and core Makefile.

**Tasks**:
- [x] Create `.gitignore` (adapt template's: `.env`, `coverage*.out`, plus `frontend/node_modules/`, `ports/web/dist/`, `data/`)
- [x] `go mod init github.com/m4schini/splitkauf` (Go 1.25); add `tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen`
- [x] Write `splitkauf.openapi.yaml`: info (title "Splitkauf API", license TODO), `servers: [{url: /api/v1}]`, `GET /health` → 200 `{status: string, checks: {database: string}}` (schema `HealthStatus`; `database` reports `"disabled"` until Phase 2)
- [x] Copy/adapt `config/` from template (`config.go`, `defaults.go`, `validation.go`, `config.yaml`): `ServiceName = "splitkauf"`, env prefix `SPLITKAUF_`, structs `App`/`Server`/`Metrics` unchanged
- [x] Copy/adapt `telemetry/log.go` and `telemetry/metrics/{metrics.go,middleware.go,server.go}` (namespace from `config.ServiceName`)
- [x] Copy/adapt `ports/rest/{server.go,api-catalog.go,docs.go}` and `ports/rest/middleware/logging.go`
- [x] Create `ports/rest/v1/config.yaml` (chi-server + models → gen.go) and `ports/rest/v1/api.go` with `//go:generate go tool oapi-codegen -config config.yaml ../../../splitkauf.openapi.yaml`, `New()`, and `type V1 struct{}` implementing `GetHealth`
- [x] Create `client/config.yaml` + `client/gen.go` (generate directive against `../splitkauf.openapi.yaml`)
- [x] Run generation; commit `ports/rest/v1/gen.go` and `client/client.gen.go` (generated files are committed, as in the template)
- [x] `main.go`: `//go:embed splitkauf.openapi.yaml`, `rest.SetOpenAPISpec`, `cmd.Execute()`
- [x] `cmd/root.go`: cobra root `Use: config.ServiceName`, `cobra.OnInitialize(config.Load)`, root `RunE` returns `cmd.Help()`
- [x] `cmd/serve.go`: `serve` subcommand — start metrics server when enabled, then `http.ListenAndServe(host:port, rest.New(&v1.V1{}))` with graceful shutdown on SIGINT/SIGTERM
- [x] Write `Makefile` from template minus `verify-template`; file rules target `splitkauf.openapi.yaml`; keep `generate/build/test/test-unit/coverage/fmt/fmt-check/lint/lint-fix/lint-vet/tidy/tidy-check/security/deps/help`
- [x] Add `ports/rest/v1/api_test.go`: httptest against `rest.New(&v1.V1{})` asserting `GET /api/v1/health` → 200 with `status == "ok"`
- [x] Commit as `feat: bootstrap Go backend skeleton` (with `Assisted-by:` trailer per AGENTS.md)

**Automated Verification**:
- [x] `make generate && git diff --exit-code ports/rest/v1/gen.go client/client.gen.go` (generation is clean/committed)
- [x] `make build` succeeds
- [x] `make test` passes (health handler test)
- [x] `make lint`, `make fmt-check`, `make tidy-check` pass; `make security` — see Implementation Notes
- [x] `grep -r "ais-schule\|TEMPLATE" --exclude-dir={docs,.git,node_modules,dist} .` returns nothing (case-sensitive)

**Manual Verification**:
- [x] `go run . serve` then `curl localhost:8080/api/v1/health` returns `{"status":"ok",...}`; `/docs` renders the Scalar UI

### Phase 2: PostgreSQL Wiring

Dependencies: Phase 1

Database config, connection adapter, embedded golang-migrate migrations, `migrate` subcommand, postgres in docker-compose, DB-aware health.

**Tasks**:
- [x] Extend `config/`: `DatabaseConfig{Host, Port, User, Password, Name, SSLMode}` with `DSN() string` (libpq-style, as in community-exchange `config/config.go` ~lines 103–109), defaults (localhost:5432, sslmode=disable, name/user `splitkauf`), validation (port range; non-empty name/user). Pool-tuning fields (`MaxOpenConns` etc. from exchange) are intentionally omitted until needed.
- [x] `adapters/db/db.go`: `NewSQL(dsn string) (*sql.DB, error)` using `_ "github.com/jackc/pgx/v5/stdlib"`, `sql.Open("pgx", dsn)` + `PingContext` with timeout; on ping failure it returns the opened handle **and** the error (callers decide severity)
- [x] `database/migrations.go`: adapt community-exchange (`Migrate`, `MigrateDown`, `OverrideDirty`, `newMigrator` with `iofs` + `//go:embed migrations`); use local `telemetry.Logger`; drop the `migration_error.go` helper (log via `zap.Error` directly)
- [x] `database/migrations/000001_init.up.sql`: `CREATE EXTENSION IF NOT EXISTS pgcrypto;` and `000001_init.down.sql`: `DROP EXTENSION IF EXISTS pgcrypto;`
- [x] `cmd/migrate.go`: adapt community-exchange `cmd/migrate.go` (`--version`, `--force`, `--dangerously-destroy-database`)
- [x] `cmd/serve.go`: open `*sql.DB` via `adapters/db` before starting the server; on ping error log a warning and start anyway (health reports degraded; server must not crash-loop while the DB is briefly down); pass the handle into `v1.V1{DB: ...}`. `migrate` in contrast treats any DB error as fatal
- [x] `GetHealth`: nil-guard the DB handle, then ping with 1s timeout; `checks.database = "ok"|"error"`, overall `status = "ok"|"degraded"` (HTTP 200 either way; spec updated + regenerated)
- [x] `docker-compose.yaml`: add `postgres:17` service with volume, healthcheck (`pg_isready`), env (`POSTGRES_DB/USER/PASSWORD=splitkauf`), port 5432
- [x] Unit tests: `DatabaseConfig.DSN()` formatting; health handler with nil DB reports `database: "error"`, status `degraded`
- [x] Commit as `feat: add postgres wiring and migrations`

**Automated Verification**:
- [x] `make generate && make build && make test && make lint` pass
- [x] `docker compose up -d postgres && go run . migrate` exits 0; second run logs "no migration necessary"
- [x] `go run . migrate --dangerously-destroy-database` exits 0 (schema teardown works); `go run . migrate` re-applies
- [x] With postgres up and `go run . serve`: `curl localhost:8080/api/v1/health` shows `"database":"ok"`; with postgres stopped: `"degraded"`

### Phase 3: Frontend, PWA Shell, and Embedding

Dependencies: Phase 1 (Phase 2 only for the health payload shown in the UI)

React app in `frontend/`, PWA shell per the iOS research, embedded and served by the Go binary.

**Tasks**:
- [x] Scaffold `frontend/` with `npm create vite@latest frontend -- --template react-ts`; commit `package-lock.json`
- [x] `frontend/vite.config.ts`: `server.proxy['/api'] = 'http://localhost:8080'`; `build.outDir = '../ports/web/dist'`, `emptyOutDir: true`
- [x] Add `vite-plugin-pwa`: `registerType: 'autoUpdate'`, manifest `{name: "Splitkauf", short_name: "Splitkauf", display: "standalone", start_url: "/", background_color: "#ffffff", theme_color: "#007aff", icons: [192, 512, 512-maskable]}`; precache app shell only (default globPatterns)
- [x] `frontend/index.html`: iOS meta tags per research — `apple-mobile-web-app-capable`, `apple-mobile-web-app-status-bar-style`, `apple-mobile-web-app-title`, `apple-touch-icon` (180px), `viewport-fit=cover`
- [x] Add placeholder icons `frontend/public/icons/icon-{192,512,512-maskable,180}.png` (simple generated solid-color PNGs; real branding later)
- [x] Minimal `src/App.tsx`: fetch `/api/v1/health`, render service status ("Splitkauf — API: ok / degraded") — proves proxy and embedding end-to-end
- [x] Add npm script `typecheck`: `tsc -b --noEmit` (the Vite react-ts scaffold uses solution-style tsconfig with project references, so `-b` is required)
- [x] `ports/web/web.go`: `//go:embed all:dist`, `Handler() http.Handler` serving files with SPA fallback (unknown non-`/api` path → `dist/index.html`, correct Content-Type, no fallback for paths with file extensions → 404)
- [x] `ports/rest/server.go`: mount `web.Handler()` at `/` (chi `NotFound`/catch-all after `/api/v1`, `/openapi.yaml`, `/docs`)
- [x] Makefile: `frontend-deps` (`npm ci --prefix frontend`), `frontend-build` (`npm run build --prefix frontend`), stub rule `ports/web/dist/index.html:` (creates placeholder if missing) as prerequisite of `generate`; `build` chain stays `generate → go build ./...` (compile check, stub embedded); new `dist` target: `frontend-build generate` then `go build -o splitkauf .` (release binary with real frontend)
- [x] Go test in `ports/web/web_test.go`: `GET /` returns 200 `text/html`; `GET /some/spa/route` falls back to index.html; `GET /api/v1/health` still hits the API (via `rest.New`)
- [x] Commit as `feat: add react pwa frontend served from go binary`

**Automated Verification**:
- [x] `npm run build --prefix frontend` succeeds and populates `ports/web/dist/`
- [x] `make build` succeeds from a clean checkout (stub rule); `make dist` produces `./splitkauf` embedding the real frontend
- [x] `make test` passes including `ports/web` tests
- [x] `npm --prefix frontend run typecheck` passes

**Manual Verification**:
- [x] `go run . serve` (after `frontend-build`) serves the app at `localhost:8080`, health status visible
- [x] `npm run dev --prefix frontend` hot-reloads and proxies `/api` to the running Go server
- [x] Lighthouse/DevTools "Application" tab shows a valid manifest and registered service worker

### Phase 4: Development Harness

Dependencies: Phases 1–3 (hooks/checks cover both stacks)

Backpressure at edit time (Claude hooks), commit time (pre-commit), and on demand (`make check`).

**Tasks**:
- [x] `.golangci.yml`: baseline config (govet, staticcheck, errcheck, revive, gosec) with generated-file exclusions (`gen.go`, `client.gen.go`)
- [x] Frontend tooling: keep Vite's oxlint config (deviation from plan — see Implementation Notes), add `prettier` + `.prettierrc`, add `vitest` with one smoke test (`App` renders); npm scripts `lint`, `format` (`prettier --write`), `format-check`, `test` (`vitest run`) (`typecheck` already exists from Phase 3)
- [x] `.pre-commit-config.yaml` from template (trailing-whitespace, end-of-file-fixer, check-yaml, check-added-large-files, check-merge-conflict, detect-private-key, check-json, gitleaks) with `default_install_hook_types: [pre-commit, commit-msg]` (otherwise the commit-msg hook is never installed) plus local hooks:
  - `gofumpt -w` on staged `.go` files; `golangci-lint run --fast` when `.go` files staged — this hook entry runs `make ports/web/dist/index.html` first so the embed stub exists outside make
  - `oxlint --fix` + `prettier --write` on staged `frontend/**/*.{ts,tsx,css,json}` (fix-mode on both, symmetric with gofumpt; oxlint instead of eslint per Phase 3 scaffold deviation)
  - commit-msg hook enforcing `^(feat|fix|chore)(\([a-z0-9./-]+\))?!?: .+` (AGENTS.md conventional-commit subset)
- [x] `hack/format-file.sh <file>`: dispatch by extension — `.go` → `go run mvdan.cc/gofumpt@v0.7.0 -w`; files under `frontend/` (`.ts/.tsx/.css/.json/.md`) → `npm --prefix frontend exec -- prettier --write <file>` plus `npm --prefix frontend exec -- oxlint --fix <file>` for ts(x) (oxlint instead of eslint per Phase 3 deviation; note: `npx --prefix` does NOT resolve local bins — must use `npm exec`); exit non-zero with a stderr message if the linter still reports errors (so the agent sees the failure); normalizes absolute paths (as passed by the Claude hook) to repo-relative before dispatch
- [x] `.claude/settings.json` (committed project settings): `PostToolUse` hook matching `Edit|Write` running `hack/format-file.sh` on the edited file path (extracts `tool_input.file_path` from the hook's stdin JSON via `jq`)
- [x] Makefile: `frontend-check` (`lint`, `format-check`, `typecheck`, `test`) and `check: fmt-check lint lint-vet tidy-check test-unit security frontend-check` (already present from Phase 1's forward-looking Makefile)
- [x] Document harness usage in `README.md` (install via `pre-commit install --install-hooks` — installs both stages thanks to `default_install_hook_types`; `make check`; hook behavior)
- [x] Commit as `chore: add development harness (pre-commit, claude hooks, make check)`

**Automated Verification**:
- [x] `pre-commit run --all-files` passes
- [x] `make check` passes (fmt-check's `git diff` step still shows the pre-existing false positive on a dirty tree noted in Phase 1 — verified via `gofumpt -l .` returning empty; every other sub-target passes, including the known Go 1.26.3 stdlib CVEs from Phase 1 in `security`)
- [x] Commit-msg hook check via temp files: `echo "bad message" > /tmp/msg && pre-commit run conventional-commit --hook-stage commit-msg --commit-msg-filename /tmp/msg` fails; same with `feat: x` passes
- [x] `hack/format-file.sh` on a deliberately misformatted `.go` file (in a temp copy) reformats it (scripted check)

### Phase 5: CI, Container, and Deployment Artifacts

Dependencies: Phases 1–4

GitHub Actions mirroring the local gates, production Dockerfile, full docker-compose, renovate, podman quadlets.

**Tasks**:
- [x] `Dockerfile` (multi-stage): `node:22` stage → `npm ci && npm run build` in `frontend/` (outDir redirected into the build context copy); `golang:1.26` builder → copy dist into `ports/web/dist`, `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`; final `gcr.io/distroless/static:nonroot`, `ENTRYPOINT ["/app"]`, `CMD ["serve"]`
- [x] `.dockerignore`: `frontend/node_modules`, `ports/web/dist` (a stale local dist must never shadow the node-stage build), `.git`, `data`, `coverage*`, `docs`
- [x] `docker-compose.yaml`: `app` service (build context `.`, `command: ["serve"]` implicit via CMD, env `SPLITKAUF_DATABASE_*`, `depends_on: postgres: condition: service_healthy`, port 8080) alongside the Phase-2 postgres service
- [x] `.github/workflows/ci.yml`: on PR + push to main; jobs:
  - `backend`: setup-go (go-version-file), `make generate` + `git diff --exit-code`, `make build`, `make test-unit`
  - `frontend`: setup-node 22 with npm cache, `npm ci`, `npm run lint`, `npm run typecheck`, `npm run test`, `npm run build`
  - `docker`: `docker build .` (no push)
- [x] `.github/workflows/lint.yml` — standalone workflow triggered independently on PR + push to main (mirrors the template's separate-workflow layout; no `workflow_call` wiring): `make fmt-check`, `make lint`, `make tidy-check`, `make security`
- [x] `renovate.json`: base config `config:recommended`; **keep the template's `go-yit` pin rule** (yaml-jsonpath v4 incompatibility breaks oapi-codegen generation)
- [x] `deploy/quadlet/splitkauf.network`: named podman network
- [x] `deploy/quadlet/splitkauf-db.volume` + `deploy/quadlet/splitkauf-db.container`: `Image=docker.io/library/postgres:17`, `Volume=splitkauf-db.volume:/var/lib/postgresql/data`, `Network=splitkauf.network`, `EnvironmentFile=/etc/splitkauf/db.env`, `HealthCmd=pg_isready`
- [x] `deploy/quadlet/splitkauf.container`: app image, `Exec=serve`, `Network=splitkauf.network`, `PublishPort=8080:8080`, `EnvironmentFile=/etc/splitkauf/splitkauf.env`, `[Unit] Requires/After=splitkauf-db.service`, `[Install] WantedBy=default.target`
- [x] `deploy/README.md`: server install steps (copy units to `/etc/containers/systemd/`, create env files, `systemctl daemon-reload`, `systemctl start splitkauf`) and one-shot migration via `podman run ... splitkauf migrate`
- [x] Commit as `chore: add ci, dockerfile, compose and quadlet deployment`

**Automated Verification**:
- [x] `docker build .` succeeds locally
- [x] `docker compose config -q` validates
- [x] `docker compose up -d --build && curl -f localhost:8080/api/v1/health && curl -sf localhost:8080/ | grep -qi splitkauf` (app + db healthy end-to-end), then `docker compose down -v` — verified via a port-remapped copy (`15432`/`18080`) since the dev machine's host ports 5432/8080 are occupied by an unrelated ssh tunnel/local process (same constraint as Phase 2); also ran `migrate` against the compose stack for good measure
- [x] `actionlint` passes on `.github/workflows/` (via `go run github.com/rhysd/actionlint/cmd/actionlint@latest`)

**Manual Verification**:
- [x] Push to GitHub: all CI jobs green
- [x] On the target server: quadlet units generate services (`systemctl daemon-reload` shows `splitkauf.service`, `splitkauf-db.service`), app reachable

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

**Phase 1:** `make security` (govulncheck) reports 3 Go stdlib vulnerabilities (GO-2026-5856, GO-2026-5039, GO-2026-5037) fixed in Go 1.26.4/1.26.5 — these stem from the local machine's installed Go 1.26.3 toolchain, not from repository code; `go.mod` only pins `go 1.25.0` as a minimum. Not addressed here since it requires a host toolchain upgrade outside this plan's scope; CI (Phase 5) should pin an unaffected Go patch version via `setup-go`.

**Phase 2:** The dev machine already had an unrelated postgres process/container bound to host port 5432, so `docker compose up -d postgres` (which also maps 5432) could not be verified directly. Migration/health verification instead used a disposable `docker run postgres:17` container on host port 15432 with matching env, driven via `SPLITKAUF_DATABASE_PORT`/`SPLITKAUF_DATABASE_PASSWORD` overrides — same code path, different port. `docker-compose.yaml` itself keeps the plan's standard `5432:5432` mapping and passed `docker compose config -q`.

**Phase 3:** The current `npm create vite@latest -- --template react-ts` scaffold ships `oxlint` (`.oxlintrc.json`, `"lint": "oxlint"`) instead of the ESLint flat config the plan's Phase 4 tasks assume — Vite switched its default template linter since the plan was written. Kept oxlint rather than manually swapping in ESLint, since it's what the scaffold now provides by default; Phase 4's "keep Vite's eslint flat config" task will need to add `prettier` alongside oxlint instead of ESLint, and any `eslint --fix` steps (pre-commit hook, `hack/format-file.sh`) become `oxlint --fix` (or oxlint's fix flag) when Phase 4 is implemented.

**Phase 4:** Before this phase, a runtime log surfaced a real bug in `DatabaseConfig.DSN()` (Phase 1): an empty password produced an unquoted `password= dbname=x` DSN, which pgx's libpq-style parser misparsed, swallowing `dbname` into the empty password and leaving the database name empty. Fixed by quoting/escaping every DSN value; committed separately as `fix: quote DSN values so an empty password does not swallow dbname` (not part of this phase's task list, but landed during it). Also, enabling the `gitleaks` pre-commit hook flagged two `stripe-access-token` matches in the pre-existing `.claude/skills/go-review` reference docs — inspected and confirmed as an illustrative fake token in a code example about avoiding secret leakage in JSON logs, not a real credential. Resolved via a `.gitleaks.toml` allowlist entry for that file's path (chosen over a `.gitleaksignore` fingerprint suppression, per user preference) rather than committing it unexamined.

## References

- `docs/research/go-backend-community-template.md` — template architecture (source of Phase 1 structure)
- `docs/agents/research/2026-07-21-pwa-ios-support.md` — manifest, iOS meta tags, install-prompt constraints (Phase 3)
- `docs/research/collaborative-lists.md` — future feature plans (not this plan)
- `docs/agents/research/2026-07-21-oidc-go-pwa-integration.md` — future auth plan (deps intentionally not added yet)
- `/Users/malte.schink/Projects/AIS/community-template` — Makefile, Dockerfile, config/, telemetry/, ports/rest/, .pre-commit-config.yaml, workflows
- `/Users/malte.schink/Projects/AIS/community-exchange/database/migrations.go`, `cmd/migrate.go`, `adapters/db/db.go` — golang-migrate wiring
- AGENTS.md — commit conventions (`feat|fix|chore`, `Assisted-by:` trailer, plans committed as `chore(plans)` in their own commit)
