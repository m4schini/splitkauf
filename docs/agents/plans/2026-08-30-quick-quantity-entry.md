---
date: 2026-08-30T09:50:44+00:00
git_commit: cae6bc682844b180fe9d907015085be10f7362de
branch: main
topic: "Enter a quantity directly: typeable stepper and per-unit presets"
tags: [plan, frontend, lists, quantity, units]
status: ready
---

# PLAN: Enter a quantity directly (US-L.12)

Setting "200 g" today means tapping the quick-add `+` button 199 times: the
quantity between `−`/`+` is a read-only `<span>`, the step is always 1, and
there is no other way to enter a number before adding. This plan replaces the
read-only value with a typeable number input, and adds a row of one-tap preset
chips whose values depend on the selected unit (100/250/500/1000 for `g` and
`ml`, 1/2/5 for `kg` and `l`, none for count-like units). Both the quick-add
bar and the inline edit form use the same new `QuantityField` component.

The change is frontend-only. The quantity stays an integer ≥ 1 across the
OpenAPI spec, domain, and database; no API, migration, or Go change is
involved. Based on
`docs/agents/research/2026-08-30-how-units-are-set-in-ux-ui.md`.

## Acceptance Criteria

- The quick-add quantity is a typeable number input (`type="number"`,
  `inputmode="numeric"`) between the `−`/`+` buttons; typing "200" needs no
  button taps.
- `−`/`+` step by 1 with a minimum of 1 (unchanged); `−` is disabled at 1.
- Preset chips are shown under the quantity control only for units that have
  presets: `g`/`ml`: 100 · 250 · 500 · 1000; `kg`/`l`: 1 · 2 · 5; `amount`
  and the count-like units (`pack`, `bottle`, `can`, `jar`, `cup`, `bunch`,
  `bag`): none. Tapping a chip replaces the quantity; the chip whose value
  equals the current quantity is marked pressed (`aria-pressed`).
- Changing the unit keeps the current quantity; nothing is auto-set.
- While typing, the field may be empty or invalid. The parent form's quantity
  is only ever a valid integer: each keystroke that parses to an integer ≥ 1
  (after `Math.floor`) is committed immediately; empty, `0`, negative, or
  non-numeric drafts leave the last valid value in place. On blur the field
  snaps back to that value. Submit is never blocked.
- Quick-add and the edit form share one `QuantityField` component; the edit
  form gains the stepper and chips.
- The name-only chained add is unchanged: the quantity field never takes
  focus unless tapped; after Add the controls reset to 1 × amount and focus
  returns to the name input (existing tests stay green).
- All controls are ≥ 44 px tall, chips ≥ 44 px, everything labeled (no
  placeholder-as-label), readable in light and dark mode.
- Documentation: new user story `docs/user-stories/US-L.12-enter-quantity-directly.md`,
  README story index gains an M9 row.

## Technical Key Decisions and Tradeoffs

1. **Typeable stepper plus per-unit preset chips** (not natural-language
   parsing of the name, not a unit-aware step size).
   - Why: typing covers any value; chips cover the common ones in one tap.
     Keeps the M6 decision "quick-add controls, not natural-language parsing"
     intact; a unit-aware step would need snapping rules and surprises users
     who typed a non-multiple.
   - Impact: a new `QuantityField` component; a `unitPresets` map keyed by
     `Unit` next to `units`/`unitLabels` in `frontend/src/api.ts`.
2. **Step stays 1.**
   - Why: chips and typing handle big jumps; no snapping edge cases.
   - Impact: the existing stepper-bound test keeps its semantics.
3. **Draft string inside the component; parent only sees valid integers.**
   - Why: per-keystroke clamping (`Number(v) || 1`, as the edit form does
     today) makes clearing the field snap to `1`, so typing "200" becomes
     "1200". A local draft lets the field be empty mid-typing while the
     parent state always holds a submittable value.
   - Impact: `QuantityField` owns `draft: string | null`; `onChange(n)` fires
     only for valid parses; blur clears the draft so the display snaps to the
     committed value. Form submission needs no extra parsing step.
4. **One shared component for quick-add and edit form.**
   - Why: identical UX in both places, one test surface.
   - Impact: the edit form's plain `<input type="number">` is replaced; the
     `edit-qty-{id}` id and visible "Quantity" label are preserved so the
     existing `getByLabelText` queries still resolve.
5. **Frontend-only; quantity remains an integer ≥ 1.**
   - Why: decimals ("1.5 kg") would touch the spec, the DB, and the domain;
     out of scope. "1500 g" covers the need.
   - Impact: `kg`/`l` presets are coarse (1 · 2 · 5) by design.
6. **Docs: new story US-L.12 and an M9 README row; US-L.9 untouched.**
   - Why: story IDs are stable; US-L.9 documents what was true at M6.

## Current State

```
Quick-add (frontend/src/ListDetail.tsx:499-556)
┌──────────────────────────────────────┐
│ Add item                             │
│ [ e.g. Milk              ] [ Add ]   │
│ [−] 1 [+]   Unit [Stück ▾]           │   ← "1" is a <span>, not an input
└──────────────────────────────────────┘
  −/+ : setItemQuantity(q ± 1), min 1, step 1   (ListDetail.tsx:518-539)
  after Add: reset to 1 × amount, refocus name  (ListDetail.tsx:248-255)

Edit form (frontend/src/ListDetail.tsx:87-143)
  Quantity: <input type="number" min=1 onChange={Number(v) || 1}>   ← typeable, glitchy on clear
  Unit:     <select>

Item row: formatQuantity(quantity, unit) → "3" / "2 l" / "500 g"  (ListDetail.tsx:43-48)
```

- `frontend/src/api.ts:169-207` — `Unit`, `units[]`, `unitLabels{}`.
- `frontend/src/index.css:383-440` — `.quick-add-controls`, `.stepper*`,
  `.quick-add-unit*` styles; `:442-475` — `.item-edit-form` input styles.
- `frontend/src/ListDetail.test.tsx:327-422` — quick-add quantity/unit tests
  that read the stepper value via `getByTestId('quick-add-quantity')` and
  `toHaveTextContent(...)`.
- Quantity contract: `splitkauf.openapi.yaml:553-557` (`integer`,
  `minimum: 1`), `lists/lists.go:242-251` (`normalizeQuantity`).

## Desired End State

```
Quick-add
┌──────────────────────────────────────┐
│ Add item                             │
│ [ e.g. Milk              ] [ Add ]   │
│ [−] [ 250 ] [+]   Unit [g ▾]         │   ← input type=number, inputmode=numeric
│ (100) (250) (500) (1000)             │   ← chips, only for g/kg/ml/l; (250) pressed
└──────────────────────────────────────┘

Quick-add with unit = Stück (no chips)
│ [−] [ 1 ] [+]   Unit [Stück ▾]       │

Edit form
│ Item name  [ Mehl            ]       │
│ Quantity   [−] [ 500 ] [+]           │
│            (100) (250) (500) (1000)  │
│ Unit       [g ▾]                     │
│ Note       [ Optional note   ]       │
│ [ Save ] [ Cancel ]                  │
```

```
frontend/src/api.ts
  Unit, units[], unitLabels{}, unitPresets{}      ← new map
        │
        ▼
frontend/src/QuantityField.tsx                    ← new component
  <div class="quantity-field">
    <div class="stepper"> [−] <input type=number> [+] </div>
    <div class="quantity-presets"> chip × n </div>   (only if unitPresets[unit].length > 0)
        │  onChange(n: number)   — valid integers only
        ▼
frontend/src/ListDetail.tsx
  quick-add:  <QuantityField id="new-item-quantity" value={itemQuantity} unit={itemUnit} …>
  edit form:  <QuantityField id={`edit-qty-${item.id}`} label="Quantity" value={quantity} unit={unit} …>
```

## Abstractions and Code Reuse

- `frontend`
  - `src/api.ts` — add `unitPresets: Record<Unit, readonly number[]>` beside
    `unitLabels`; doc comment states the values and that count-like units are
    deliberately empty.
  - `src/QuantityField.tsx` — new. Props:
    ```ts
    interface QuantityFieldProps {
      /** id of the number input; pairs with the visible or implicit label. */
      id: string
      /** Visible label text; when omitted the input carries aria-label="Quantity". */
      label?: string
      value: number
      unit: Unit
      /** Called only with a valid integer ≥ 1. */
      onChange: (quantity: number) => void
      /** Optional data-testid for the number input (quick-add keeps `quick-add-quantity`). */
      inputTestId?: string
    }
    ```
    Internals: `draft: string | null` state; `parseQuantity(draft)` helper
    (exported for unit tests) returning `number | null`; `−`/`+` buttons with
    the existing `aria-label`s "Decrease quantity"/"Increase quantity" and
    `stepper-button` class; chips rendered as `<button type="button"
    aria-pressed>` inside `<div role="group" aria-label="Quantity presets">`.
  - `src/ListDetail.tsx` — quick-add stepper markup (lines 518–539) and the
    edit form's quantity input (lines 105–112) replaced by `QuantityField`;
    `data-testid="quick-add-quantity"` moves onto the quick-add input.
  - `src/index.css` — `.stepper-value` replaced by `.stepper-input`; new
    `.quantity-field`, `.quantity-presets`, `.preset-chip`,
    `.preset-chip[aria-pressed="true"]`.
  - `src/QuantityField.test.tsx` — new component tests.
  - `src/ListDetail.test.tsx` — quick-add assertions switch from
    `toHaveTextContent` to `toHaveValue`; new integration tests for typing and
    chips.
- `docs`
  - `user-stories/US-L.12-enter-quantity-directly.md` — new story.
  - `user-stories/README.md` — `### M9 — Quick-add polish` table with row 21.

## Logging & Observability

None. Purely client-side interaction; no new network calls or events.

## Implementation

### Phase 1: Typeable QuantityField in quick-add and edit form

Dependencies: None.

Replace the read-only stepper value with a number input via a shared
`QuantityField` component, wire it into both forms, and document the feature
(the user story describes the complete feature including Phase 2's chips).

**Tasks**:
- [x] Create `frontend/src/QuantityField.tsx` with `QuantityFieldProps` as
      above (accept and ignore `unit` for now; Phase 2 uses it). Export
      `parseQuantity(draft: string): number | null`:
      ```ts
      export function parseQuantity(draft: string): number | null {
        const n = Math.floor(Number(draft.trim()))
        return draft.trim() !== '' && Number.isFinite(n) && n >= 1 ? n : null
      }
      ```
      Render:
      ```tsx
      <div className="quantity-field">
        {label && <label htmlFor={id}>{label}</label>}
        <div className="stepper" role="group" aria-label="Quantity">
          <button type="button" className="stepper-button" aria-label="Decrease quantity"
                  disabled={value <= 1} onClick={() => commit(value - 1)}>−</button>
          <input id={id} className="stepper-input" type="number" inputMode="numeric"
                 min={1} step={1} aria-label={label ? undefined : 'Quantity'}
                 data-testid={inputTestId}
                 value={draft ?? String(value)}
                 onChange={(e) => { setDraft(e.target.value); const n = parseQuantity(e.target.value); if (n !== null) onChange(n) }}
                 onBlur={() => setDraft(null)} />
          <button type="button" className="stepper-button" aria-label="Increase quantity"
                  onClick={() => commit(value + 1)}>+</button>
        </div>
      </div>
      ```
      where `commit(n)` calls `setDraft(null)` then `onChange(Math.max(1, n))`.
      Test-query gotcha: while an item is being edited, the quick-add input
      (`aria-label="Quantity"`) and the edit form's labeled input both answer
      to "Quantity". Tests must query the quick-add input via
      `getByTestId('quick-add-quantity')` and the edit input via
      `getByLabelText('Quantity', { selector: '#edit-qty-i1' })` (same pattern
      the existing unit test uses for `#edit-unit-i1`).
- [x] `frontend/src/ListDetail.tsx` quick-add: replace the `<div
      className="stepper">…</div>` block (lines 518–539) with
      `<QuantityField id="new-item-quantity" inputTestId="quick-add-quantity"
      value={itemQuantity} unit={itemUnit} onChange={setItemQuantity} />`.
      Keep the reset to `1` and the `itemInputRef.current?.focus()` in
      `handleAddItem` unchanged.
- [x] `frontend/src/ListDetail.tsx` edit form: replace the `<label
      htmlFor={`edit-qty-${item.id}`}>Quantity</label>` + `<input
      type="number">` pair (lines 105–112) with `<QuantityField
      id={`edit-qty-${item.id}`} label="Quantity" value={quantity} unit={unit}
      onChange={setQuantity} />`.
- [x] `frontend/src/index.css`: replace `.stepper-value` with `.stepper-input`
      (`width: 72px; min-height: 44px; text-align: center; font-size: 16px;
      border/background/color as `.quick-add-unit`; hide the native spin
      buttons via `appearance: textfield` and the `::-webkit-outer-spin-button`
      / `::-webkit-inner-spin-button` `appearance: none` rules). Add
      `.quantity-field { display: flex; flex-direction: column; gap: 8px; }`.
      Scope the `.item-edit-form input` rule so it does not fight the
      `.stepper-input` width (`.item-edit-form > input, …`, or give
      `.stepper-input` a more specific selector).
- [x] `frontend/src/QuantityField.test.tsx`:
      - `parseQuantity`: `'200' → 200`, `' 7 ' → 7`, `'1.9' → 1`, `'' → null`,
        `'0' → null`, `'-3' → null`, `'abc' → null`.
      - Typing: clear the field and type `200`; `onChange` was last called
        with `200`; the input shows `200`.
      - Empty draft: clear the field; `onChange` not called with an invalid
        value; input shows empty; on blur the input shows the prop value.
      - `−` disabled at 1; `+` calls `onChange(value + 1)`; `−` calls
        `onChange(value - 1)`.
      - Accessible name: without `label` the input is found by
        `getByLabelText('Quantity')`; with `label="Quantity"` the visible
        label resolves to the input.
- [x] `frontend/src/ListDetail.test.tsx`: change the four
      `getByTestId('quick-add-quantity')` `toHaveTextContent('n')` assertions
      (lines 372, 414, 417, 420) to `toHaveValue(n)` (a number, since the
      input is `type="number"`). Add:
      - "adds an item with a typed quantity": type `Bread`, clear the quantity
        input and type `200`, select `g`, click Add; POST body has
        `{ quantity: 200, unit: 'g' }`; row shows `200 g`; quantity input
        resets to `1`.
      - The existing "adds an item optimistically and keeps the quick-add
        input focused" test (line 93) covers the US-L.4 chained-add flow —
        it must pass untouched; no new test needed.
      - "editing an item can type a new quantity": edit `Milk`, clear
        `getByLabelText('Quantity', { selector: '#edit-qty-i1' })`, type
        `12`, Save; PATCH body has `{ quantity: 12 }`.
- [x] Create `docs/user-stories/US-L.12-enter-quantity-directly.md`
      (Milestone M9, depends on US-L.9). Acceptance criteria mirror the list
      above, including the preset chips table and the ASCII mockups from
      *Desired End State*.
- [x] `docs/user-stories/README.md`: add
      `### M9 — Quick-add polish` with row 21 → US-L.12 ("Typing a quantity
      and one-tap presets replace tapping `+` 199 times for 200 g; builds on
      the US-L.9 controls").
- [x] Commit the phase (code, tests, user story, README) as one commit:
      `feat(frontend): typeable quantity field in quick-add and edit form`.
      User stories are neither research documents nor plans, so they ship
      with the code that implements them.

**Automated Verification**:
- [x] `cd frontend && npx vitest run src/QuantityField.test.tsx` passes.
- [x] `cd frontend && npx vitest run src/ListDetail.test.tsx` passes,
      including the unchanged chained-add focus test.
- [x] `make frontend-check` green (oxlint, prettier, tsc, vitest).
- [ ] `make check` green from a clean tree. (Blocked by a pre-existing
      `ports/rest` failure that also occurs on an unmodified tree — see
      *Implementation Notes*.)

**Manual Verification**:
- [x] On a phone: tap the quick-add quantity, the numeric keyboard opens,
      type `200`, choose `g`, tap Add — the row reads "200 g" and the
      controls reset to 1 × Stück.
- [x] Type "milk↵ eggs↵ bread↵" without touching the quantity — focus stays
      on the name field the whole time.
- [x] Edit an item, clear the quantity field, type `12`, Save — row shows 12.
- [x] Light and dark mode: the number input matches the unit select's look.

### Phase 2: Per-unit preset chips

Dependencies: Phase 1.

Add the `unitPresets` map and render one-tap chips inside `QuantityField`
whenever the selected unit has presets.

**Tasks**:
- [x] `frontend/src/api.ts`: add after `unitLabels`
      ```ts
      /**
       * One-tap quantity presets per unit, shown as chips under the quantity
       * field (US-L.12). Weight/volume units get the common pack sizes;
       * `amount` and the count-like units have none — the stepper is enough
       * and the quick-add bar stays uncluttered. Quantities are integers, so
       * kg/l presets are coarse by design ("1.5 kg" is entered as 1500 g).
       */
      export const unitPresets: Record<Unit, readonly number[]> = {
        amount: [],
        g: [100, 250, 500, 1000],
        kg: [1, 2, 5],
        ml: [100, 250, 500, 1000],
        l: [1, 2, 5],
        pack: [], bottle: [], can: [], jar: [], cup: [], bunch: [], bag: [],
      }
      ```
- [x] `frontend/src/QuantityField.tsx`: after the stepper, when
      `unitPresets[unit].length > 0`, render
      ```tsx
      <div className="quantity-presets" role="group" aria-label="Quantity presets">
        {unitPresets[unit].map((preset) => (
          <button key={preset} type="button" className="preset-chip"
                  aria-pressed={value === preset} onClick={() => commit(preset)}>
            {preset}
          </button>
        ))}
      </div>
      ```
      Changing `unit` re-renders the chip row only; `value` is untouched.
- [x] `frontend/src/index.css`: `.quantity-presets { display: flex; gap: 8px;
      flex-wrap: wrap; }`; `.preset-chip` — `min-height: 44px; padding: 8px
      12px; border-radius: 22px; border: 1px solid var(--control-border);
      background: var(--bg-elevated); color: var(--fg); font-size: 16px;`;
      `.preset-chip[aria-pressed="true"] { background: var(--accent); color:
      var(--accent-contrast); border-color: var(--accent); }`;
      `.preset-chip:hover:not([aria-pressed="true"]) { background:
      var(--bg-elevated-hover); }`.
- [x] `frontend/src/api.test.ts`: `unitPresets` has an entry for every value
      in `units`; every preset is an integer ≥ 1; `g`/`ml` are
      `[100, 250, 500, 1000]`, `kg`/`l` are `[1, 2, 5]`, all others empty.
- [x] `frontend/src/QuantityField.test.tsx`:
      - `unit="amount"`: no `Quantity presets` group rendered.
      - `unit="g"`: chips `100 250 500 1000` rendered; clicking `250` calls
        `onChange(250)`; with `value={250}` the `250` chip has
        `aria-pressed="true"` and the others `"false"`.
      - Chip click while a draft is pending (field cleared, then click `500`)
        shows `500` in the input.
- [x] `frontend/src/ListDetail.test.tsx`:
      - "preset chips follow the selected unit": initially no chips; select
        `g` → chips appear, quantity still `1`; click `500` → input shows
        `500`; select `l` → chips are `1 2 5`, input still shows `500`
        (unit change keeps the quantity); click Add → POST body
        `{ quantity: 500, unit: 'l' }`; after Add no chips are shown
        (unit reset to `amount`).
      - Edit form: editing an item with `unit: 'g'` shows the chips; clicking
        `1000` then Save PATCHes `{ quantity: 1000 }`.
- [x] `docs/user-stories/US-L.12-enter-quantity-directly.md` already
      describes the chips (written in Phase 1); no doc change in this phase.
      Commit as `feat(frontend): per-unit quantity preset chips`.

**Automated Verification**:
- [x] `cd frontend && npx vitest run src/api.test.ts src/QuantityField.test.tsx src/ListDetail.test.tsx` passes.
- [x] `make frontend-check` green.
- [ ] `make check` green from a clean tree. (Same pre-existing `ports/rest`
      failure as in Phase 1 — see *Implementation Notes*.)

**Manual Verification**:
- [x] On a phone: choose `g`, tap `250`, tap Add — "250 g" in one gesture
      chain; choose `Stück` — no chip row, bar height unchanged from today.
- [x] Chip states are distinguishable in light and dark mode (pressed chip
      uses the accent, unpressed is elevated); text contrast ≥ 4.5:1.
- [x] Chips wrap rather than overflow on a narrow (320 px) viewport.

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

**Phase 1**

- `make check` fails in `go test ./ports/rest` (`TestOpenAPISpecJSONHandler`,
  `TestYAMLToJSONTaggedScalars`, `TestYAMLToJSONErrors`). The same three tests
  fail on an unmodified tree (verified via `git stash -u`), so the failure
  predates this plan and is unrelated to the frontend-only change.
  `make frontend-check` is green.
- The stepper's wrapper no longer carries `role="group" aria-label="Quantity"`.
  With the value now an input that is itself named "Quantity", the group made
  `getByLabelText('Quantity')` ambiguous (two elements with that name) while
  adding nothing — each button already has its own label.
- `oxlint` warns `react(only-export-components)` on `QuantityField.tsx`,
  because `parseQuantity` is exported next to the component. Kept as planned
  (the parser is unit-tested); it is a warning, not an error, and
  `make frontend-check` passes.
- `.item-edit-form input`/`select` were narrowed to direct children so the
  edit form's generic input styling no longer applies to the quantity input
  nested inside `.quantity-field`.

**Phase 2**

- No deviations. The chip row is a `role="group" aria-label="Quantity presets"`
  as planned, which the tests use to scope chip queries while both the
  quick-add bar and an open edit form are on screen.

## References

- `docs/agents/research/2026-08-30-how-units-are-set-in-ux-ui.md` — where
  units and quantities are set today
- `docs/user-stories/US-L.9-quantity-and-unit.md` — original quick-add
  controls
- `docs/agents/plans/2026-08-01-m6-branding-quantity-unit.md` — key decision
  2 ("controls, not natural-language parsing"), stepper implementation notes
- `docs/agents/research/2026-07-31-mobile-first-shopping-list-ux.md` §6 —
  UX checklist (≥44 px targets, keyboard stays open, labels not placeholders)
- `frontend/src/ListDetail.tsx:87-143,499-556` — edit form and quick-add
- `frontend/src/api.ts:169-207` — `Unit`, `units`, `unitLabels`
- `frontend/src/index.css:383-475` — stepper and edit-form styles
- `splitkauf.openapi.yaml:553-557`, `lists/lists.go:242-251` — quantity
  contract (integer ≥ 1)
