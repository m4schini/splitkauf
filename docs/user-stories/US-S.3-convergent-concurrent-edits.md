# US-S.3 — Convergent concurrent edits

**Milestone:** M3
**Depends on:** US-S.1

**As a** member editing at the same time as someone else, **I want** our changes to
converge without data loss, **so that** the list stays trustworthy.

## Acceptance criteria

- Last-write-wins for item fields is acceptable.
- Check/uncheck operations are never silently dropped.
