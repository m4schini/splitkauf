# US-O.3 — Sync when back online

**Milestone:** M4
**Depends on:** US-O.2

**As a** member coming back online, **I want** my queued offline changes to sync
automatically, **so that** I don't have to redo anything.

## Acceptance criteria

- Local changes queue and replay on reconnect (background sync where supported).
- Conflicts resolve per the sync rules (US-S.3).
