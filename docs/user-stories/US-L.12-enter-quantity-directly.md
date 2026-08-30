# US-L.12 — Enter a quantity directly

**Milestone:** M9
**Depends on:** US-L.9

**As a** member adding items, **I want** to type a quantity and tap common
values, **so that** "200 g" takes one entry instead of 199 taps on the `+`
button.

## Acceptance criteria

- The quick-add quantity between the `−`/`+` buttons is a typeable number
  input (`type="number"`, `inputmode="numeric"`, so a phone opens the numeric
  keyboard). Typing "200" needs no button taps.
- `−`/`+` still step by 1 with a minimum of 1; `−` is disabled at 1. The step
  is deliberately unit-independent — typing and the presets cover big jumps,
  and a unit-aware step would surprise anyone who typed a non-multiple.
- One-tap preset chips sit under the quantity control, but only for units that
  have presets. Tapping a chip replaces the quantity; the chip matching the
  current quantity is marked pressed (`aria-pressed`).

  | Unit | Presets |
  |------|---------|
  | `g`, `ml` | 100 · 250 · 500 · 1000 |
  | `kg`, `l` | 1 · 2 · 5 |
  | `amount`, `pack`, `bottle`, `can`, `jar`, `cup`, `bunch`, `bag` | none — the stepper is enough and the bar stays uncluttered |

  ```
  ┌──────────────────────────────────────┐
  │ Add item                             │
  │ [ e.g. Milk              ] [ Add ]   │
  │ [−] [ 250 ] [+]   Unit [g ▾]         │
  │ (100) (250) (500) (1000)             │   ← (250) pressed
  └──────────────────────────────────────┘

  Unit = Stück — no chip row, bar height unchanged:
  │ [−] [ 1 ] [+]   Unit [Stück ▾]       │
  ```

- Changing the unit keeps the current quantity; nothing is auto-set or
  converted.
- While typing, the field may be empty or invalid without disturbing the form:
  each keystroke that parses to an integer ≥ 1 (after flooring) is committed
  immediately, while empty, `0`, negative or non-numeric drafts leave the last
  valid value in place. On blur the field snaps back to that value. Submit is
  never blocked.
- Quantities stay integers ≥ 1 across the OpenAPI spec, the domain and the
  database: no decimals ("1.5 kg" is entered as 1500 g), so the `kg`/`l`
  presets are coarse by design. The change is frontend-only — no API,
  migration or server change.
- The edit form uses the same control as the quick-add bar: stepper, typed
  quantity and the same per-unit chips.

  ```
  │ Item name  [ Mehl            ]       │
  │ Quantity   [−] [ 500 ] [+]           │
  │            (100) (250) (500) (1000)  │
  │ Unit       [g ▾]                     │
  │ Note       [ Optional note   ]       │
  │ [ Save ] [ Cancel ]                  │
  ```

- The name-only chained add (US-L.4) is unaffected: the quantity field never
  takes focus unless tapped, and after Add the controls reset to 1 × Stück
  with focus back on the name input.
- All controls are ≥44px tall, chips included; every control is labeled (no
  placeholder-as-label) and legible in light and dark mode, with chips
  wrapping rather than overflowing on a 320px viewport.
