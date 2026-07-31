# US-A.3 — Stay logged in

**Milestone:** M2
**Depends on:** US-A.2

**As a** member, **I want** my session refreshed transparently, **so that** I'm not
forced to re-login mid-shop.

## Acceptance criteria

- Backend refreshes tokens server-side; session cookie rotation as designed in the
  OIDC research doc.
