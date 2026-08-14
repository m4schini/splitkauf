# Research: Go Backend Architecture (community-template)

## Summary

`community-template` is a production-ready Go service template built around **hexagonal (ports & adapters) architecture**. It enforces strict layering: domain logic in `app/`, inbound contracts (REST) in `ports/`, outbound integrations in `adapters/`, thin Cobra CLI wiring in `cmd/`, and shared infrastructure (config, telemetry) in their own packages.

The REST API is **spec-first**: `TEMPLATE.openapi.yaml` is the single source of truth. `oapi-codegen` generates both the chi-based server stub (`ports/rest/v1/gen.go`) and a typed HTTP client (`client/client.gen.go`) from the same spec. Generated files are never hand-edited.

---

## Key File Map

```
community-template/
├── main.go                         # Embed OpenAPI spec; rest.SetOpenAPISpec; cmd.Execute()
├── TEMPLATE.openapi.yaml           # Source of truth for REST API
├── Makefile                        # generate / build / test / fmt / lint / tidy / security
├── Dockerfile                      # Multi-stage, distroless/static, CGO_ENABLED=0
├── go.mod                          # Go 1.25; chi, cobra, viper, zap, prometheus, oapi-codegen
│
├── cmd/root.go                     # Cobra root; OnInitialize → config.Load()
│
├── config/
│   ├── config.go                   # Config struct; Load() singleton (Viper + sync.Once)
│   ├── config.yaml                 # Local dev overrides
│   ├── defaults.go                 # setDefaults(v *viper.Viper)
│   └── validation.go               # validate(*Config) → errors.Join
│
├── ports/rest/
│   ├── server.go                   # New(si) chi router; mounts /api/v1 with middlewares
│   ├── api-catalog.go              # RFC 9727 catalog; /openapi.yaml, /openapi.json, /docs
│   ├── docs.go                     # Scalar UI (scalar-go)
│   ├── middleware/logging.go       # Zap structured request logging
│   └── v1/
│       ├── config.yaml             # oapi-codegen: chi-server + models → gen.go
│       ├── api.go                  # go:generate directive; V1 struct implements ServerInterface
│       └── gen.go                  # GENERATED: ServerInterface, ChiServerOptions, HandlerWithOptions
│
├── client/
│   ├── config.yaml                 # oapi-codegen: client + models → client.gen.go
│   ├── gen.go                      # go:generate directive; package doc
│   └── client.gen.go              # GENERATED: Client, ClientWithResponses
│
├── telemetry/
│   ├── log.go                      # Logger(names...) → named Zap, init-once via sync.OnceFunc
│   └── metrics/
│       ├── metrics.go              # Custom Prometheus registry; RED metrics; build_info
│       ├── middleware.go           # chi middleware recording RED metrics by route pattern
│       └── server.go               # NewServer(host,port,path) → *http.Server for scrape
│
├── adapters/                       # (empty; intended for db/, gitea/, etc.)
├── app/                            # (empty; intended for service/, repository/)
└── hack/template.sh                # Rename TEMPLATE → newname throughout codebase
```

---

## Architecture Layers

```
Request → chi router (/api/v1)
          → metrics.Middleware (RED increment, labeled by chi route pattern)
          → middleware.Logging (Zap: method, path, route, status, duration)
          → generated route handler (ServerInterfaceWrapper in gen.go)
          → V1 method in ports/rest/v1/api.go
          → app/service/...
          → app/repository/ interface
          → adapters/db/ (pgx, future)

main.go → cmd.Execute()
  └── cobra.OnInitialize → config.Load()  (Viper singleton, validated)
        └── subcommand RunE → ports/rest/server.New(v1.V1{})
```

**Import rules** (enforced by convention):
- `app/` must NOT import `ports/` or `adapters/`
- `ports/` must NOT import `adapters/`
- `cmd/` calls into `app/` and reads `config.C`

---

## Detailed Findings

### Entry Point (`main.go`)
Two things only: embed `TEMPLATE.openapi.yaml` at compile time via `//go:embed`, register it with `rest.SetOpenAPISpec(openAPISpec)`, then call `cmd.Execute()`. The binary is self-contained — the spec it serves is the exact spec it was compiled against.

### Cobra CLI (`cmd/root.go`)
`cobra.OnInitialize` calls `config.Load()` before any command executes, giving all subcommands a fully populated `config.C`. Subcommands (e.g., `serve`, `migrate`) are added as separate files via `rootCmd.AddCommand(...)`.

### Configuration (`config/`)
Viper singleton exposed as `config.C *Config`. Three-tier precedence:
1. Env vars prefixed `TEMPLATE_` (dots → underscores)
2. Config file at `./config/config.yaml`, `/etc/template/config.yaml`, or XDG config home
3. Hard-coded defaults in `config/defaults.go`

Struct: `Config{ App AppConfig, Server ServerConfig, Metrics MetricsConfig }`. Validation uses `errors.Join` to collect all failures before returning. Adding a new setting requires changes to all three files plus the YAML.

### Code Generation (oapi-codegen)
Two pipelines, one spec source (`TEMPLATE.openapi.yaml`):

| Config file | Output | Generates |
|---|---|---|
| `ports/rest/v1/config.yaml` | `ports/rest/v1/gen.go` | chi server stub + models |
| `client/config.yaml` | `client/client.gen.go` | HTTP client + models |

`oapi-codegen` is declared as a `tool` dependency in `go.mod` (Go 1.25 tool directive). Triggered by `make generate`, which is also a prerequisite of `make build` and `make test`.

### REST Handler Pattern
`gen.go` generates `type ServerInterface interface{}`. `api.go` defines `type V1 struct{}` which must implement `ServerInterface`. When the spec gains new endpoints and `gen.go` is regenerated, missing methods on `V1` produce compile errors immediately.

```
TEMPLATE.openapi.yaml → oapi-codegen → ServerInterface (gen.go)
                                              ↑ implements
                                       V1{} in api.go  →  app/service/...
```

### Telemetry (Logging)
`telemetry.Logger(names ...string) *zap.Logger` is the only logging entry point. It initializes Zap once via `sync.OnceFunc`, picks dev vs. production config from `config.C.App.Debug`, sets level from `config.C.App.LogLevel`. Using `zap.NewProduction()` directly or the standard `log` package is forbidden.

### Telemetry (Metrics)
A **custom Prometheus registry** (not `DefaultRegisterer`) is used. Collectors: `http_requests_total`, `http_request_duration_seconds`, `http_response_size_bytes`, `http_requests_in_flight`, `build_info`, Go runtime collector, process collector. The metrics server is an independent `*http.Server` on port 9090 — completely separate from the API server. The middleware labels by `chi.RouteContext` route pattern, falling back to `"unmatched"` for 404s to bound cardinality.

### Makefile
`make generate` → `make build` → `make test` is the main chain. `make test-unit` runs with `-race -short`. `make fmt-check` and `make tidy-check` are CI guards.

### Dockerfile
Multi-stage: `golang:1.26` builder with `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`, then `distroless/static:nonroot` runtime. Generated files are committed so no `go generate` step is needed in Docker.

### Scaffolding
`hack/template.sh splitkauf` renames all `TEMPLATE` placeholders throughout file contents and filenames. The go module path becomes `github.com/ais-schule/community-splitkauf`, the env prefix becomes `SPLITKAUF_`, the OpenAPI file becomes `splitkauf.openapi.yaml`.

---

## Key Code References

- `main.go:10` — `//go:embed TEMPLATE.openapi.yaml` and `rest.SetOpenAPISpec`
- `cmd/root.go:33` — `cobra.OnInitialize(func() { cobra.CheckErr(config.Load()) })`
- `config/config.go:20-47` — `Config`, `AppConfig`, `ServerConfig`, `MetricsConfig` structs
- `config/config.go:56-102` — `Load()` singleton with Viper, env prefix, unmarshal, validate
- `config/validation.go:10-45` — `validate()` with `errors.Join`
- `ports/rest/server.go:14-24` — `New(si)` assembles chi router with `/api/v1` mount and middlewares
- `ports/rest/v1/api.go:3` — `//go:generate go tool oapi-codegen -config config.yaml ...`
- `ports/rest/v1/api.go:14` — `type V1 struct{}` — the handler receiver
- `telemetry/log.go:13` — `initLogger = sync.OnceFunc(...)` init-once Zap
- `telemetry/metrics/metrics.go:21` — `Registry = prometheus.NewRegistry()`
- `telemetry/metrics/middleware.go:17-41` — `Middleware` with route-pattern labeling
- `Makefile:35-44` — file-rule generate targets

---

## Patterns Directly Applicable to Splitkauf

1. **Run `hack/template.sh splitkauf`** first — renames all placeholders, updates module path, env prefix, OpenAPI file name.
2. **Spec-first REST**: Write `splitkauf.openapi.yaml` before any handler code. `make generate` → implement on `V1` struct → compile error if diverged.
3. **Config**: Add a `Database DatabaseConfig` sub-struct to `Config` for pgx settings; follow the three-file pattern (`config.go`, `defaults.go`, `validation.go`).
4. **Logger**: Only `telemetry.Logger("component")` — never `zap.L()` directly or `log.*`.
5. **Metrics**: Register domain collectors in `telemetry/metrics/metrics.go` using `config.ServiceName` as namespace.
6. **pgx adapter**: Create `adapters/db/` for all SQL. Parameterized queries only, `defer rows.Close()`, check `rows.Err()`, transactions for multi-statement writes.
7. **Subcommands**: Add `cmd/serve.go` (HTTP server) and `cmd/migrate.go` (DB migrations) as Cobra subcommands.
8. **docker-compose**: Add a `postgres:` service block — the template's `docker-compose.yaml` currently has none.

---

## Open Questions

- How pgx connection pooling and migration tooling (`golang-migrate` or `goose`) should be wired into `adapters/db/` is not defined in the template.
- The `cmd/root.go` `RunE` is a no-op placeholder — decide whether the default action starts the server or requires an explicit `serve` subcommand.
- The `docker-compose.yaml` has no PostgreSQL service — needs to be added for local development of splitkauf.
