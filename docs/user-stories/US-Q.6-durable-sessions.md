# US-Q.6 — Durable sessions

**Milestone:** M5
**Depends on:** US-A.2, US-A.3

**As an** operator, **I want** the instance to refuse to start in OIDC mode
without a reachable database, **so that** member sessions are never silently
held in process memory and lost on restart.

## Acceptance criteria

- In OIDC mode, `serve` fails fast at startup with a clear error when the
  database is unreachable (sessions must be durable; logins cannot complete
  without the members table anyway).
- In dev-auth mode, the existing in-memory session-store fallback is kept so
  local development still serves while the database is down.
- The deployment README documents the behavior.

## Origin

Deferred finding from the M2 security review: the Postgres-vs-memory session
store is chosen once at startup (`cmd/serve.go`), so a database outage at boot
silently pins sessions to process memory for the whole process lifetime.
