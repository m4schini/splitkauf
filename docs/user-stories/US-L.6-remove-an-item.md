# US-L.6 — Remove an item

**Milestone:** M1
**Depends on:** US-L.4

**As a** member, **I want** to remove an item that's no longer needed, **so that**
the shopper doesn't buy it.

## Acceptance criteria

- The item disappears for all members.
- No confirmation dialog: removal is immediate with an undo snackbar (~5s);
  soft delete under the hood (UX research §2).
- If removal is offered via swipe, a visible tap alternative exists
  (WCAG 2.5.1).
