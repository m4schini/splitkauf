# US-Q.2 — Uniform errors

**Milestone:** cross-cutting (baseline in M1)
**Depends on:** US-Q.1
**Plan:** `docs/agents/plans/2026-07-21-rfc9457-error-responses.md` (status: ready)

**As a** frontend developer, **I want** all error responses as RFC 9457 Problem
Details, **so that** error handling is a single code path.

## Acceptance criteria

From the implementation plan:

- Every error the API emits (router binding errors, request-validation failures,
  404/405 on `/api/v1`, panics) is an RFC 9457 body with
  `Content-Type: application/problem+json`.
- Every emitted `type` is a path-absolute URI `/problems/{slug}` whose page
  resolves to a human-readable HTML explanation served by the Go backend
  (`about:blank` is never used).
- A registry test proves every emitted problem type has a page (no drift).
- The OpenAPI spec declares `ProblemDetail` plus a reusable `default` `Problem`
  response referenced by every operation; server and client artifacts are
  regenerated.
- Handler panics no longer kill the connection and never leak internals in the
  response body.
- The frontend has a fetch wrapper (`frontend/src/api.ts`) that throws a typed
  `ProblemDetail` on `application/problem+json` errors.

## Implementation approach (per the plan)

- In-house `ports/rest/problem` package (no library) with a registry of four
  problem types: `validation`, `not-found`, `method-not-allowed`, `internal`.
- All four error surfaces wired: custom `ErrorHandlerFunc`, chi 404/405 via
  `BaseRouter`, panic-recovery middleware, and OpenAPI request-validation
  middleware — so future endpoints get validation and uniform errors for free.
- HTML explanation pages at `/problems/{slug}`, rendered from the registry.
- Four plan phases: problem foundation → error surfaces → problem pages →
  frontend fetch wrapper (one commit per phase, per `AGENTS.md`).

## References

- Research: `docs/agents/research/2026-07-21-rfc9457-problem-details.md`
- [RFC 9457 — Problem Details for HTTP APIs](https://www.rfc-editor.org/rfc/rfc9457.html)
