# US-L.7 — Check off an item

**Milestone:** M1
**Depends on:** US-L.4

**As a** member shopping in the store, **I want** to check an item off, **so that**
everyone sees it's in the cart.

## Acceptance criteria

- Checked items are visually separated from open items.
- A check/uncheck is never silently dropped, even under concurrent edits.
- Who checked the item and when is recorded (enables undo, US-L.8).
- The **entire row** is the tap target (≥48dp tall), one-handed in-store use;
  no confirmation dialog; check-off feedback is instantaneous, never gated on
  the network (UX research §1–2).
- Checked items move to a collapsed "done" section at the bottom rather than
  disappearing, and their dimmed text stays ≥4.5:1 contrast.
