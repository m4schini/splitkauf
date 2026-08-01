# US-O.3 — Sync when back online

**Milestone:** M4
**Depends on:** US-O.2

**As a** member coming back online, **I want** my queued offline changes to sync
automatically, **so that** I don't have to redo anything.

## Acceptance criteria

- Local changes queue and replay on reconnect. Replay triggers on the
  `online` and `visibilitychange` events (iOS Safari has no Background Sync
  API — see `docs/agents/research/2026-07-21-pwa-ios-support.md`).
- Conflicts resolve per the sync rules (US-S.3).
- A queued change whose target was deleted in the meantime (replay returns
  404) is dropped: the server state wins, the affected data is refetched, and
  a quiet notice tells the member — never a modal or a silent divergence.
