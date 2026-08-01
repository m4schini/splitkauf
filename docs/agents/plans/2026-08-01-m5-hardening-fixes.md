---
date: 2026-08-01T05:45:40+00:00
git_commit: 19cdedfd9160059dc9208d4a2d7d5b5cba7a7523
branch: main
topic: "M5: hardening fixes (TOCTOU, body limits, session fail-fast)"
tags: [plan, m5, hardening, rest, db, sessions]
status: ready
---

# PLAN: M5 — Hardening Fixes

Close the three code-level findings deferred from the M1–M2 reviews, now
captured as stories US-Q.5 (request body limits), US-Q.6 (durable sessions),
and US-Q.7 (concurrent-delete correctness).

**Scope decisions (from the user):** fixes only — the US-Q.4 umbrella audit
(UX/accessibility pass, real-device walkthrough, production config review)
gets its own plan later, closer to a real deployment. The manual OIDC
verification against a live provider is likewise deferred (ad hoc when a real
deployment exists), so it appears in no phase here.

**Ordering note:** the M4 plan (`2026-08-01-m4-offline-first.md`) lands first
(milestone order) and also modifies `adapters/db/lists.go` `AddItem` (adds a
`checked` parameter). The line references below are against the shared base
commit (`19cdedf`) — re-anchor them after M4; the Phase 1 change itself is
unaffected (drop the pre-check, map the FK violation).

## Acceptance Criteria

- **US-Q.7**: `AddItem` no longer pre-checks list existence; a foreign-key
  violation (Postgres `23503`) from the insert maps to `lists.ErrNotFound`
  → 404. Adding to a nonexistent list still returns 404.
- **US-Q.5**: every `/api/v1` request body is capped at 1 MiB; an oversized
  `Content-Length` is rejected with 413 before the body is read; a
  `payload-too-large` problem type is registered with a resolving explanation
  page (drift test covers it).
- **US-Q.6**: in OIDC mode, `serve` fails fast with a clear error when the
  database is unreachable at startup; dev-auth mode keeps the in-memory
  fallback; the deploy README documents it.
- `make check` green; one commit per story.

## Technical Key Decisions and Tradeoffs

1. **Map the FK violation instead of pre-checking (US-Q.7):** drop
   `requireList` from `AddItem`; on insert error, `errors.As` into
   `*pgconn.PgError` and map code `23503` to `ErrNotFound`.
   - Why: the check-then-insert is the race; the constraint is the authority.
   - Impact: one fewer round-trip per add; `pgconn` becomes a direct import of
     `adapters/db` (already in the module via pgx).
2. **Two-layer body limit (US-Q.5):** a middleware on the API subrouter that
   (a) rejects a declared `Content-Length` > 1 MiB with an immediate 413
   problem and (b) installs `http.MaxBytesReader` as the backstop for
   chunked/streamed bodies.
   - Why: the fast path gives a clean, deterministic 413; the backstop
     guarantees the cap even without a declared length. A backstop-triggered
     failure surfaces through the OpenAPI validator as a 400 validation
     problem — acceptable (the cap held; only the status differs), noted in
     the middleware comment.
   - Impact: new `problem.PayloadTooLarge` (413) type + `FromStatus` mapping;
     page and drift test come free from the registry. The middleware applies
     to the whole `/api/v1` subrouter (uniform, covers the body-less SSE
     route harmlessly). The `/api/auth/*` routes are mounted outside the API
     subrouter and stay uncapped — in scope for US-Q.5 they are fine: login/
     callback are GETs and logout is a tiny form POST with no JSON body.
3. **Fail-fast decision extracted for testability (US-Q.6):** a small pure
   function decides `postgres | memory | fatal` from
   (`IsOIDCEnabled`, DB reachable), unit-tested; `serve` acts on it.
   - Why: `serve()` itself is not unit-testable; the policy is.
   - Impact: OIDC deployments crash-loop visibly (systemd/podman restart
     policy applies) instead of serving amnesiac sessions.

## Current State

```
adapters/db/lists.go:154   AddItem: requireList() pre-check → INSERT (TOCTOU)
ports/rest/v1/handlers_lists.go:129,198  io.ReadAll / json.Decode, unbounded
cmd/serve.go:59            newSessionManager: DB down at boot → in-memory
                           store for the whole process lifetime, both modes
ports/rest/problem/        registry: 400/401/404/405/500/503 (no 413)
```

## Desired End State

- A concurrent `DeleteList` during `AddItem` yields 404, never 500.
- `curl -H 'Content-Length: 2000000' …` → `413 application/problem+json`
  `type:/problems/payload-too-large`; `/problems/payload-too-large` renders.
- OIDC mode + unreachable DB → process exits with
  `sessions require a reachable database in OIDC mode`; dev mode unchanged.

## Abstractions and Code Reuse

- `ports/rest/problem/problem.go` — add `PayloadTooLarge` (413) to the
  registry and `FromStatus`; pages/drift test need no changes.
- `ports/rest/middleware/` — new `maxbody.go` following `recover.go`'s
  middleware pattern.
- `cmd/serve.go` — `sessionStore(oidcEnabled, dbReachable bool)` decision
  helper next to `newSessionManager`.

## Logging & Observability

- 413s flow through the existing logging middleware like any problem.
- The fatal OIDC/no-DB path logs the error at startup before exiting;
  the dev-mode in-memory warning (`cmd/serve.go:69`) stays.

## Implementation

### Phase 1: US-Q.7 — FK-violation mapping in AddItem

Dependencies: none

**Tasks**:
- [ ] `adapters/db/lists.go`: remove the `requireList` call from `AddItem`
      (delete the helper if it has no other callers); on insert error,
      `errors.As(err, &pgErr)` with `*pgconn.PgError` and return
      `lists.ErrNotFound` for code `23503`; update the doc comment that
      currently (incorrectly) claims the FK path produces the domain error.
- [ ] `adapters/db/lists_test.go`: integration test — `AddItem` to a
      well-formed but nonexistent list ID returns `ErrNotFound` (this now
      exercises the FK path); existing add tests stay green.

**Automated Verification**:
- [ ] Integration tests pass against a disposable Postgres (schema ≥ v2);
      `go test -short ./...`, `make lint` green.

### Phase 2: US-Q.5 — request body limits + 413 problem type

Dependencies: none

**Tasks**:
- [ ] `ports/rest/problem/problem.go`: register `PayloadTooLarge`
      (slug `payload-too-large`, status 413, description); map 413 in
      `FromStatus`; extend `problem_test.go`.
- [ ] `ports/rest/middleware/maxbody.go`: `MaxBody(limit int64)` middleware —
      declared `Content-Length > limit` → `problem.Write` 413 and stop;
      otherwise `r.Body = http.MaxBytesReader(w, r.Body, limit)` and
      delegate. Comment the backstop-status caveat (Key Decision 2).
- [ ] `ports/rest/server.go`: `apiRouter.Use(middleware.MaxBody(1 << 20))`
      (before the generated group, alongside `Recover`).
- [ ] Tests (`ports/rest` or `ports/rest/v1`): oversized `Content-Length` →
      413 problem with the right `type`; a body over the cap without a
      declared length is rejected (status 400 or 413, both problem+json);
      a normal-size POST still succeeds; drift test passes with the new type.

**Automated Verification**:
- [ ] `go test ./ports/...`, `make lint` green.

### Phase 3: US-Q.6 — session-store fail-fast in OIDC mode

Dependencies: none

**Tasks**:
- [ ] `cmd/serve.go`: add `sessionStore(oidcEnabled, dbReachable bool)
      (usePostgres bool, err error)` — `(true, true)` → postgres;
      `(true, false)` → error `sessions require a reachable database in OIDC
      mode`; `(false, _)` → memory fallback allowed. `serve` returns the
      error before binding; `newSessionManager` consumes the decision.
- [ ] `cmd/serve_test.go` (new): table test for `sessionStore`.
- [ ] `deploy/README.md`: document the fail-fast behavior and that the
      restart policy (systemd/podman) handles a DB that comes up late.

**Automated Verification**:
- [ ] `go test ./cmd/...` passes; `make check` green.
- [ ] Scripted behavior check without a live IdP (the DB gate must run before
      OIDC discovery, so no issuer is ever contacted):
      `SPLITKAUF_DATABASE_PORT=1 SPLITKAUF_AUTH_OIDC_ISSUER=https://example.invalid SPLITKAUF_AUTH_OIDC_CLIENT_ID=x SPLITKAUF_AUTH_OIDC_CLIENT_SECRET=x SPLITKAUF_AUTH_OIDC_REDIRECT_URL=http://localhost/cb go run . serve; test $? -ne 0`
      and the output contains `sessions require a reachable database`;
      then `SPLITKAUF_DATABASE_PORT=1 go run . serve &` (dev mode) still
      binds and `curl -s localhost:8080/api/v1/health` reports `degraded`.

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

## References

- User stories: `docs/user-stories/US-Q.5`, `US-Q.6`, `US-Q.7` (origins cite
  the exact review findings).
- Review findings: M1 go-review (TOCTOU, unbounded bodies), M2 security
  review (session-store fallback permanence).
- `docs/agents/plans/2026-07-21-rfc9457-error-responses.md` (problem registry
  conventions).
