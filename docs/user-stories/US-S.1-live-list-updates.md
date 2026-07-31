# US-S.1 — Live list updates

**Milestone:** M3
**Depends on:** US-L.7

**As a** member with the app open, **I want** to see items added, edited, checked,
or removed by others appear live, **so that** two people shopping together don't buy
things twice.

## Acceptance criteria

- Changes propagate to all connected clients via the chosen transport (SSE or
  WebSocket, per research/plan decision).
- Remote changes appear without loading states or layout jumps; collaborator
  activity is surfaced subtly (e.g. "Alex added 3 items"), never as a
  blocking interruption (UX research §2).
