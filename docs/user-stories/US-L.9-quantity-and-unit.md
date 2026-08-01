# US-L.9 — Quantity and unit

**Milestone:** M6
**Depends on:** US-L.4, US-O.2

**As a** member adding items, **I want** to set a quantity and an optional
unit right in the quick-add bar, **so that** "2 l milk" takes one gesture
instead of add-then-edit.

## Acceptance criteria

- The bottom quick-add bar gains a compact quantity stepper and a unit
  selector beside the name input (defaults: 1 × amount; all controls ≥44px,
  keyboard stays open for chained adds — the US-L.4 flow is not slowed for
  users who ignore the new controls).

  ```
  ┌──────────────────────────────────────┐
  │ Add item                             │
  │ [ Milk…            ] [− 2 +] [l ▾]   │
  │                              [ Add ] │
  └──────────────────────────────────────┘
  row shows:   □ Milk        2 l
  ```

- Units are a fixed, curated set of the most common German/European grocery
  units. Stable API enum token first, German UI label in parentheses:
  `amount` (Stück — default, rendered as a bare number), `g` (g), `kg` (kg),
  `ml` (ml), `l` (l), `pack` (Packung), `bottle` (Flasche), `can` (Dose),
  `jar` (Glas), `cup` (Becher), `bunch` (Bund), `bag` (Beutel).
- Item rows display quantity + unit ("2 l", "500 g", bare "3" for amount);
  the edit form can change both; unit follows the item through check/uncheck,
  offline queueing, and sync unchanged.
- Spec-first: `Item`/`AddItemRequest`/`UpdateItemRequest` gain the `unit`
  enum in `splitkauf.openapi.yaml`; `items.unit` is added by a migration (its
  own commit) with a matching check constraint; the offline pending-create
  payload (US-O.2) carries the unit.
