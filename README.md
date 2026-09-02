# Splitkauf

A self-hosted, collaborative shopping list for a household: shared lists that
everyone edits together, in real time, from their phones.

Splitkauf ships as a **single self-contained Go binary** — the React PWA is
built by Vite and embedded with `go:embed`. The only external runtime
dependency is PostgreSQL.

> ### Disclaimer: 100% LLM-generated
>
> This project — application code, tests, frontend, infrastructure,
> documentation and this README — was written **entirely by large language
> models** as part of a company-internal training exercise on AI-assisted
> development.

## Features

- **Shared lists and items** — create, rename, copy and delete lists; add,
  edit, check off and remove items; quantities with a curated unit set.
- **Real-time sync** — server-sent events push list changes to every open
  client; concurrent edits converge last-write-wins on the server's
  `updated_at`.
- **Offline-first PWA** — installable on iOS/Android, the React Query cache is
  persisted to IndexedDB and offline writes queue up and replay when the
  device comes back online.
- **Undo instead of confirm dialogs** — item deletes are soft deletes with a
  server-backed restore, so undo works across devices and offline.
- **Attribution** — who created a list, who added an item, who bought it;
  display names resolve at read time, so renames propagate to past actions.
- **Three auth modes** — OIDC (BFF pattern, tokens never reach the browser),
  local username/password accounts, or a hardcoded dev user for local
  development. OIDC and password can run side by side.
- **Spec-first API** — `openapi.yaml` is the source of truth; the
  chi server stubs and the typed Go client are generated from it, and errors
  are RFC 9457 Problem Details. Interactive docs at `/docs`.
- **Operations** — embedded migrations (`splitkauf migrate`), Prometheus
  metrics, structured logging, Docker image and Podman Quadlet units.

## Quick start

### With Docker Compose

```sh
git clone https://github.com/m4schini/splitkauf.git
cd splitkauf
docker compose up --build
```

This starts PostgreSQL 17, runs the migration job once and serves the app on
<http://localhost:8080>. With no OIDC issuer and no password auth configured,
the backend runs in **dev-auth mode** — a single hardcoded user, no login
credentials needed. Do not expose that to the internet.

### From source

Requirements: Go 1.26+, Node 20+, a reachable PostgreSQL.

```sh
# 1. a database
docker run -d --name splitkauf-pg -p 5432:5432 \
  -e POSTGRES_USER=splitkauf -e POSTGRES_PASSWORD=splitkauf -e POSTGRES_DB=splitkauf \
  postgres:17

# 2. schema
go run . migrate

# 3. build the single binary with the real frontend embedded, then run it
make dist
./splitkauf serve
```

`make dist` runs the frontend build and `go generate` for you. For frontend
work, run the backend with `go run . serve` and the Vite dev server with
`npm run dev --prefix frontend` — it proxies `/api` to `localhost:8080`.

### Enabling real accounts

Dev-auth is for local development only. For a real deployment pick one (or
both) of:

```sh
# local username/password accounts (no public sign-up; operator-provisioned)
export SPLITKAUF_AUTH_PASSWORD_ENABLED=true
./splitkauf user add alice

# or OIDC against any provider (Keycloak, Zitadel, …)
export SPLITKAUF_AUTH_OIDC_ISSUER=https://idp.example.com
export SPLITKAUF_AUTH_OIDC_CLIENT_ID=splitkauf
export SPLITKAUF_AUTH_OIDC_CLIENT_SECRET=…
export SPLITKAUF_APP_BASE_URL=https://splitkauf.example.com
```

`splitkauf user ls` lists every known identity and `splitkauf user merge`
moves one identity's history onto another — useful when a local account later
becomes an OIDC one.

## Configuration

Three tiers, highest first: `SPLITKAUF_*` environment variables →
`config/config.yaml` → built-in defaults. Nested keys map to underscores, so
`database.host` is `SPLITKAUF_DATABASE_HOST`. The defaults live in
`config/defaults.go`; the deployment-relevant subset is documented in
[`deploy/README.md`](deploy/README.md).

## Deployment

- **Container image**: `ghcr.io/m4schini/splitkauf` (distroless, non-root),
  `ENTRYPOINT ["/app"] CMD ["serve"]`.
- **Compose**: [`docker-compose.yaml`](docker-compose.yaml) for a
  batteries-included single-host run.
- **Production**: rootless Podman Quadlet units in
  [`deploy/quadlet/`](deploy/quadlet), with setup instructions in
  [`deploy/README.md`](deploy/README.md).

Run `splitkauf migrate` before starting a new version; migrations are embedded
in the binary.

## Documentation

- [`docs/architecture.md`](docs/architecture.md) — hexagonal layout, domain
  model, sync and auth design, with implemented vs. planned clearly marked.
- [`docs/user-stories/`](docs/user-stories) — the product scope, one story per
  file, grouped into milestones.
- [`docs/development.md`](docs/development.md) — the development harness:
  make targets, git hooks, test and coverage contract, quality dashboard.
- [`AGENTS.md`](AGENTS.md) — the rules the AI assistants work under
  (commit conventions, attribution trailers, what they must not do).
- API reference: run the app and open `/docs`, or read
  [`openapi.yaml`](openapi.yaml).

## License

[CC0 1.0 Universal](LICENSE) — public domain dedication. Do what you like
with it.
