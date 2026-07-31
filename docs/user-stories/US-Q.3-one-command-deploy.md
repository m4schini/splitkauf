# US-Q.3 — One-command deploy

**Milestone:** cross-cutting (baseline in M1)
**Depends on:** —

**As an** operator, **I want** to run the whole app (single Go binary with embedded
PWA + Postgres) via the existing compose/quadlet setup, **so that** self-hosting is
a single command.

## Acceptance criteria

- Docker image builds; compose stack boots to a working app after each milestone's
  final commit.
