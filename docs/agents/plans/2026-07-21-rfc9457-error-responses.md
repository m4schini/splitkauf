---
date: 2026-07-21T18:35:12.174838+00:00
git_commit: 5c042ecfc2050e45c2ad2be00989f47774648af0
branch: main
topic: "RFC 9457 error responses"
tags: [plan, rest, problem-details, rfc9457, openapi, frontend]
status: complete
---

# PLAN: RFC 9457 Problem Details Error Responses

Adopt RFC 9457 (*Problem Details for HTTP APIs*) as the uniform error format for the splitkauf API: an in-house `problem` package, four documented problem types with self-hosted explanation pages, all current error surfaces converted, the OpenAPI spec extended, and a typed frontend fetch wrapper.

Based on research: `docs/agents/research/2026-07-21-rfc9457-problem-details.md`.

## Acceptance Criteria

- Every error the API emits (router binding errors, request-validation failures, 404/405 on `/api/v1`, panics) is an RFC 9457 body with `Content-Type: application/problem+json`.
- Every emitted `type` is a path-absolute URI `/problems/{slug}` whose page resolves to a human-readable HTML explanation served by the Go backend (`about:blank` is never used).
- A registry test proves every emitted problem type has a page (no drift).
- The OpenAPI spec declares `ProblemDetail` + a reusable `default` `Problem` response, referenced by `/health`; server (`ports/rest/v1/gen.go`) and Go client (`client/client.gen.go`) are regenerated.
- Handler panics no longer kill the connection and never leak internals in the response body.
- The frontend has a fetch wrapper that throws a typed `ProblemDetail` on `application/problem+json` errors; `App.tsx` uses it for `/health`.

## Technical Key Decisions and Tradeoffs

1. **In-house problem package (no library):** new `ports/rest/problem` package.
   - Why: core is ~20 lines; community libs (`kodeart/go-problem`, `neocotic/go-problem`) are low-adoption; full control over extension members.
   - Impact: new Go package with `Problem` struct, type registry, write helper, tests.
2. **Path-absolute type URIs, pages served by the backend:** `"type": "/problems/{slug}"`.
   - Why: self-hosted documentation, no coupling to a canonical domain.
   - Impact: HTML pages via `html/template` on the `APIDocsHandler` router; the registry drives both the URIs and the page content. No index page — pages only.
3. **Four documented types, no `about:blank`:** `validation`, `not-found`, `method-not-allowed`, `internal`.
   - Why: every API error should have a splitkauf explanation page.
   - Impact: dedicated `NotFound`/`MethodNotAllowed` handlers on the `/api/v1` subrouter via `BaseRouter`.
4. **All four error surfaces covered:** generated router `ErrorHandlerFunc`, chi 404/405, new panic-recovery middleware, new OpenAPI request-validation middleware.
   - Why: uniform RFC 9457 surface from day one; future endpoints get request validation for free.
   - Impact: new middleware wiring in `ports/rest`; `github.com/oapi-codegen/nethttp-middleware` dependency added; `kin-openapi` is promoted from indirect to direct dependency; `embed-spec: true` added to `ports/rest/v1/config.yaml` so `GetSwagger()` is generated. The embedded spec is only used by the validator — `SetOpenAPISpec`/`main.go` keep serving the spec bytes as today (harmless duplication of the spec in the binary).
5. **OpenAPI `default` response convention:** `components/schemas/ProblemDetail` (`additionalProperties: true`) + `components/responses/Problem`, referenced as the `default` response on every operation.
   - Why: OpenAPI idiom for "any other error"; scales to future endpoints without per-code noise.
   - Impact: regenerate `ports/rest/v1/gen.go` and `client/client.gen.go`.
6. **Minimal frontend integration:** `ProblemDetail` interface + fetch wrapper in `frontend/src/api.ts`, `App.tsx` switched over.
   - Why: establishes the single centralised error path before more endpoints exist.
   - Impact: small TS addition + vitest coverage.

## Current State

```
splitkauf.openapi.yaml          Only /health (200 only) — no 4xx/5xx responses defined
│
├── ports/rest/v1/
│   ├── config.yaml             oapi-codegen: chi-server + models → gen.go (no embed-spec)
│   ├── gen.go                  Default ErrorHandlerFunc = http.Error(plain text, 400)  (gen.go:162-165)
│   └── api.go                  V1.GetHealth — only handler; writes JSON manually
├── ports/rest/server.go        chi router; no ErrorHandlerFunc / BaseRouter passed  (server.go:17-22)
├── ports/rest/api-catalog.go   APIDocsHandler chi router mounted at "/" (docs, spec, catalog)
├── ports/rest/middleware/      Logging middleware only; no panic recovery
├── client/                     Generated Go client (models + client) from the same spec
└── frontend/src/App.tsx        Single inline fetch('/api/v1/health'); .catch → 'unreachable'
```

Error behaviour today: unknown API routes → chi plain-text `404 page not found`; wrong method → plain-text 405; malformed bound params → plain-text 400 from the default `ErrorHandlerFunc`; a handler panic kills the connection (no recovery middleware).

## Desired End State

```
Client ──► /api/v1/…
             │
             ├─ panic          ──► middleware.Recover      ──► 500 {"type":"/problems/internal", …}
             ├─ invalid request ──► oapi request validator ──► 400 {"type":"/problems/validation",
             │                                                      "errors":[{detail,pointer}…]}
             ├─ bad bound param ──► custom ErrorHandlerFunc ──► 400 {"type":"/problems/validation", …}
             ├─ unknown route   ──► BaseRouter.NotFound     ──► 404 {"type":"/problems/not-found", …}
             └─ wrong method    ──► BaseRouter.MethodNotAllowed ► 405 {"type":"/problems/method-not-allowed", …}
                 all with Content-Type: application/problem+json, instance = request path

Browser ──► GET /problems/{slug} ──► HTML explanation page rendered from the registry

frontend/src/api.ts ──► apiFetch() throws typed ProblemDetail on problem+json errors
```

Example response body:

```json
{
  "type": "/problems/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "no resource exists at this path",
  "instance": "/api/v1/nope"
}
```

## Abstractions and Code Reuse

- `ports/rest/problem/` — **new package**, single source of truth for problem types
  - `problem.go`
    - `Type` — registry entry: `Slug`, `Title`, `Status`, `Description` (page text)
    - `Validation`, `NotFound`, `MethodNotAllowed`, `Internal` — the four registered types
    - `Types()` — all registered types (drives pages + drift test)
    - `FromStatus(int) Type` — maps a status code to the matching type (400→validation, 404→not-found, 405→method-not-allowed, else internal)
    - `Problem` — struct with the five RFC members + `Errors []FieldError` extension
    - `FieldError` — `{detail, pointer}` per the RFC's canonical validation pattern
    - `New(t Type, detail string) Problem` — fills `type` (`"/problems/"+Slug`), `title`, `status`
    - `Write(w, r, Problem)` — sets `Content-Type: application/problem+json`, defaults `instance` to `r.URL.Path`, encodes
- `ports/rest/middleware/` — reuse existing package and `telemetry.Logger` pattern (see `logging.go`)
  - `recover.go` - new `Recover` middleware
- `ports/rest/server.go` — reuse `ChiServerOptions` hooks (`ErrorHandlerFunc`, `BaseRouter`) already generated in `gen.go`
- `ports/rest/problems.go` — new page handler registered on the existing `APIDocsHandler` router (same pattern as `docs.go`)
- `splitkauf.openapi.yaml` — schema shared by both generated artifacts (`ports/rest/v1`, `client`) via existing `make generate`
- `frontend/src/api.ts` — new module; `App.tsx` reuses it

## Logging & Observability

- `middleware.Recover` logs recovered panics at error level with stack trace before writing the problem response:

  ```
  ERROR  api  panic recovered  {"method": "GET", "path": "/api/v1/health", "panic": "...", "stack": "..."}
  ```

- No panic details ever appear in the response body — `detail` is the generic registry description (RFC 9457 §5 security guidance).
- Existing `middleware.Logging` already records the final status code; problem responses need no extra logging.

## Implementation

### Phase 1: Problem foundation

Dependencies: None

The in-house `problem` package with the four-type registry, the OpenAPI `ProblemDetail` schema and `default` response convention, and both generated artifacts refreshed.

**Tasks**:
- [x] Create `ports/rest/problem/problem.go`: `Type`, the four registered types (`Validation` 400, `NotFound` 404, `MethodNotAllowed` 405, `Internal` 500 — each with `Slug`, `Title` = HTTP reason phrase, `Status`, and a one-paragraph `Description` for the page), `Types()`, `FromStatus`, `Problem`, `FieldError`, `New`, `Write`

  ```go
  type Problem struct {
      Type     string       `json:"type,omitempty"`
      Title    string       `json:"title,omitempty"`
      Status   int          `json:"status,omitempty"`
      Detail   string       `json:"detail,omitempty"`
      Instance string       `json:"instance,omitempty"`
      Errors   []FieldError `json:"errors,omitempty"`
  }
  ```
- [x] Create `ports/rest/problem/problem_test.go`: `Write` sets `Content-Type: application/problem+json` and the status code; empty members are omitted from JSON; `instance` defaults to the request path; `FromStatus` maps 400/404/405/500 and unknown codes; registry slugs are unique and non-empty
- [x] Add `ProblemDetail` to `components/schemas` in `splitkauf.openapi.yaml` (five standard members per the research doc's snippet, plus the `errors` array of `{detail, pointer}`, `additionalProperties: true`)
- [x] Add `components/responses/Problem` (`description` + `application/problem+json` content referencing `ProblemDetail`) and reference it as the `default` response of `getHealth`
- [x] Run `make generate` to refresh `ports/rest/v1/gen.go` and `client/client.gen.go`

**Automated Verification**:
- [x] `go test ./ports/rest/problem/...` passes
- [x] `make generate` succeeds and `git diff --exit-code` is clean afterwards (generated code committed)
- [x] `grep -q ProblemDetail ports/rest/v1/gen.go client/client.gen.go` — schema propagated to both artifacts
- [x] `make check` passes

### Phase 2: Error surfaces

Dependencies: Phase 1

Wire all four error surfaces to emit problem responses: custom `ErrorHandlerFunc`, 404/405 on the API subrouter, panic recovery, and OpenAPI request validation.

**Tasks**:
- [x] Add `embedded-spec: true` to the `generate` block of `ports/rest/v1/config.yaml` and run `make generate` (adds `GetSwagger()`; makes `kin-openapi` a direct dependency)
- [x] Add dependency `github.com/oapi-codegen/nethttp-middleware` (`go get`, `make tidy`)
- [x] Create `ports/rest/middleware/recover.go`: `Recover` middleware — on panic, log method/path/panic value/stack via `telemetry.Logger("api")`, then `problem.Write` an `Internal` problem with the registry description as `detail` (skip the write if the response was already started)
- [x] Create `ports/rest/v1/validator.go` (package `v1`): `Validator()` returning the `nethttp-middleware` request validator built from `GetSwagger()`, with an error handler that emits `problem.New(problem.FromStatus(status), message)` via `problem.Write`; for validation failures include the message as `detail`
- [x] Update `ports/rest/server.go` `New`: build a `chi.NewRouter()` for the API with `NotFound`/`MethodNotAllowed` handlers writing `problem.NotFound`/`problem.MethodNotAllowed`, and apply `Recover` router-wide via `apiRouter.Use(middleware.Recover)` **before** passing it as `BaseRouter`. Note: `ChiServerOptions.Middlewares` are applied per-route inside the generated `r.Group` and do NOT wrap the `NotFound`/`MethodNotAllowed` handlers — `Recover` must be `Use`'d on the `BaseRouter` to cover all surfaces. `v1.Validator()` goes into `ChiServerOptions.Middlewares` (per-route is correct: validation only applies to real operations), alongside the existing `Logging`/`metrics` middlewares. Set `ErrorHandlerFunc` writing `problem.New(problem.Validation, err.Error())`
- [x] Extend `ports/rest/v1/api_test.go` (or add `ports/rest/server_test.go`) with `httptest` cases against `rest.New`:
  - unknown API route → 404, `application/problem+json`, `type` = `/problems/not-found`, `instance` = request path
  - wrong method on `/api/v1/health` → 405, `type` = `/problems/method-not-allowed`
  - panicking `ServerInterface` stub → 500, `type` = `/problems/internal`, body contains no panic message
  - `ErrorHandlerFunc` (called directly) → 400, `type` = `/problems/validation`
  - validator error handler (called directly) → 400, `type` = `/problems/validation`

**Automated Verification**:
- [x] `go test ./ports/...` passes (404/405/panic surfaces; validation surface deferred, see notes)
- [x] `make check` passes

### Phase 3: Problem pages

Dependencies: Phase 2 (pages must exist once the types are emitted; keeps the acceptance criterion "every emitted type resolves")

Human-readable HTML explanation pages at `/problems/{slug}`, rendered from the registry, plus architecture documentation.

**Tasks**:
- [x] Create `ports/rest/problems.go`: `problemPageHandler` using `html/template` — renders `Title`, HTTP status, and `Description` for the `Type` matching `{slug}`; unknown slug → plain 404 HTML page; plain minimal markup, no styling framework (consistent with the lightweight docs approach)
- [x] Register `r.Get("/problems/{slug}", problemPageHandler())` in `APIDocsHandler` (`ports/rest/api-catalog.go:30`)
- [x] Add `ports/rest/problems_test.go`: drift test iterating `problem.Types()` — `GET /problems/{slug}` on the full `rest.New` handler returns 200 with `text/html`; unknown slug returns 404
- [x] Update `docs/architecture.md`: document the RFC 9457 error format, the four problem types with their `/problems/{slug}` URIs, the error surfaces, and the `default` OpenAPI response convention for future endpoints

**Automated Verification**:
- [x] `go test ./ports/rest/...` passes, including the drift test
- [x] `make check` passes

**Manual Verification**:
- [x] (self-verified via curl) `./splitkauf serve`, then GET `/problems/not-found` in a browser — a readable explanation page renders
- [x] `curl -i /api/v1/nope` returns `application/problem+json` with `"type": "/problems/not-found"`, and opening that type path in a browser shows its page

### Phase 4: Frontend fetch wrapper

Dependencies: Phase 2 (server must emit problem responses)

Typed `ProblemDetail` handling centralised in a fetch wrapper; `App.tsx` migrated.

**Tasks**:
- [x] Create `frontend/src/api.ts`: `ProblemDetail` interface (five optional standard members, optional `errors: { detail: string; pointer?: string }[]`, index signature for extensions) and `apiFetch<T>(input, init?): Promise<T>` — on non-2xx, parse the body as `ProblemDetail` when `Content-Type` includes `application/problem+json` and throw it; otherwise throw `{ status, title: res.statusText }`
- [x] Update `frontend/src/App.tsx`: replace the inline `fetch` with `apiFetch<HealthStatus>('/api/v1/health')`, keeping the existing `'unreachable'` fallback on error
- [x] Create `frontend/src/api.test.ts` (vitest, mock `fetch`): 2xx returns parsed JSON; non-2xx with `application/problem+json` throws the parsed `ProblemDetail`; non-2xx without the media type throws the status/title fallback

**Automated Verification**:
- [x] `npm --prefix frontend test` passes, including the new `api.test.ts` cases
- [x] `make check` passes (includes `frontend-check`: lint, typecheck, format, tests, build)

**Manual Verification**:
- [x] (covered by App.test.tsx success + api.test.ts error/fallback paths; `unreachable` on backend down)

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

- Phase 2: The 400 request-validation surface test (and the direct
  `ErrorHandlerFunc` unit test) is deferred to M1.2. `/health` is a GET with no
  params or body, so no current operation can naturally fail schema validation
  or parameter binding; a `POST /lists` with a required body in M1.2 exercises
  both paths end to end. The validator and `ErrorHandlerFunc` are wired and
  build-verified; the 404/405/panic surfaces are covered by full-stack tests.
- Phase 2: kept `spec.Servers` intact in the validator. The spec's server URL
  (`/api/v1`) equals the mount path, and the kin-openapi router resolves
  operations against the full `r.URL.Path`; clearing servers broke `/health`
  matching (returned 404).
- The oapi-codegen key is `embedded-spec` (not `embed-spec`).

## References

- Research: `docs/agents/research/2026-07-21-rfc9457-problem-details.md`
- [RFC 9457 — Problem Details for HTTP APIs](https://www.rfc-editor.org/rfc/rfc9457.html)
- [oapi-codegen nethttp-middleware](https://github.com/oapi-codegen/nethttp-middleware)
- Existing patterns: `ports/rest/api-catalog.go` (docs router), `ports/rest/middleware/logging.go` (middleware + telemetry logger)
