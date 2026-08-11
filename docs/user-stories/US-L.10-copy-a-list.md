# US-L.10 — Copy a list

**Milestone:** M8
**Depends on:** US-L.2

**As a** member, **I want** to copy a list with all its items unchecked, **so
that** a recurring shop starts from last time's list instead of being retyped.

## Acceptance criteria

- The list-detail header offers a Copy action next to Rename and Delete.
- The copy contains every item of the source — open and checked alike — each
  reset to unchecked, keeping name, quantity, unit, note, and display order.
  Items removed from the source do not travel with the copy.
- Without a supplied name the copy is called "«Original name» (copy)".
- Copying navigates into the copy, ready to rename or shop.
- The copy appears in every member's overview without a reload (live update).
- Copying is online-only: offline it fails with a visible, non-blocking hint
  and leaves the source list untouched — it is never queued for later.
