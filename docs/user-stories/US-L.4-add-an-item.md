# US-L.4 — Add an item

**Milestone:** M1
**Depends on:** US-L.1

**As a** member, **I want** to add an item with a name, quantity, and optional note,
**so that** whoever shops knows exactly what to buy.

## Acceptance criteria

- Name is required; quantity defaults to 1; note is optional.
- The item appears for all members.
- Quick-add is bottom-anchored (thumb reach) and the keyboard stays open with
  the input focused after each add, so items can be chained ("milk ↵ eggs ↵").
- Adding never blocks on the network — the item appears in the list
  immediately (UX research §1–2).
