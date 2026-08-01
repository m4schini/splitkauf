# US-Q.7 — Concurrent-delete correctness

**Milestone:** M5
**Depends on:** US-L.4, US-L.3

**As a** member, **I want** adding an item to a list that another member is
deleting at the same moment to fail with a clean "not found" error, **so that**
a routine race never surfaces as an internal server error.

## Acceptance criteria

- `AddItem` no longer pre-checks list existence (TOCTOU); a foreign-key
  violation from the insert (Postgres error 23503) is mapped to the domain
  `ErrNotFound`, surfacing as a 404 problem.
- Adding an item to a nonexistent list still returns 404 (now via the
  FK-violation path).

## Origin

Deferred finding from the M1 go-review: `adapters/db/lists.go` `AddItem` does
a check-then-insert; a concurrent `DeleteList` between the two turns the raw
FK violation into a 500 instead of a 404.
