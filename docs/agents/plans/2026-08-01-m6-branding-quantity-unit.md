---
date: 2026-08-01T05:45:40+00:00
git_commit: 19cdedfd9160059dc9208d4a2d7d5b5cba7a7523
branch: main
topic: "M6: branding (icon, green accent) and quick-add quantity/unit"
tags: [plan, m6, branding, icons, theme, lists, quantity, unit, frontend]
status: ready
---

# PLAN: M6 — Branding & Quick-Add Polish

Deliver the M6 slice: the real app icon everywhere (US-B.1), an icon-matched
green accent across both themes (US-B.2), and quantity + unit controls in the
quick-add bar backed by a spec-first `unit` field (US-L.9).

Lands **after M4 and M5** (README order step 16); US-L.9 retrofits `unit`
into M4's offline pending-create payload. If implemented before M4 ships,
that single task adapts (the payload module won't exist yet — record in
Implementation Notes).

## Product decisions (from the user)

1. **Area B, three stories** — US-B.1 (icon), US-B.2 (accent), US-L.9
   (quantity/unit); independent of each other.
2. **Quick-add controls, not natural-language parsing** — a compact stepper
   and unit selector beside the name input.
3. **Curated German/European grocery unit set** (fixed enum): `amount`
   (Stück, default, rendered bare), `g`, `kg`, `ml`, `l`, `pack` (Packung),
   `bottle` (Flasche), `can` (Dose), `jar` (Glas), `cup` (Becher), `bunch`
   (Bund), `bag` (Beutel).

## Acceptance Criteria

- **US-B.1**: manifest icons (192/512/512-maskable), apple-touch-icon (180),
  and the favicon are all derived from `frontend/src/assets/appicon.png`;
  the maskable variant keeps the artwork inside the safe zone; a real-device
  install shows the icon (manual).
- **US-B.2**: the accent (buttons, checkboxes, focus rings, links, checked
  states) is a green derived from the icon (≈ `#7abf7e`), with shades chosen
  so every accent pairing meets WCAG 2.2 AA (4.5:1 text, 3:1 UI) in light
  *and* dark mode; manifest `theme_color` and the iOS status-bar treatment
  follow; no meaning by color alone.
- **US-L.9**: quick-add gains a ≥44px quantity stepper + unit selector
  (default 1 × amount) without slowing the name-only flow (keyboard stays
  open, chained adds unchanged); item rows render "2 l" / "500 g" / bare "3"
  for amount; the edit form changes both; `unit` flows through the spec, DB
  (own-commit migration), handlers, offline payload, and events.
- `make check` green; one commit per story (+ the migration commit).

## Technical Key Decisions and Tradeoffs

1. **Icons generated from the 1024px source, committed as artifacts.** A
   small Node script (sharp, devDependency or one-off `npm exec`) renders the
   sizes; the maskable variant composites the artwork at ~80% onto the icon
   green so mask shapes never clip it.
   - Why: deterministic regeneration; no designer round-trip.
   - Impact: `appicon.png` (876 KB) exceeds pre-commit's
     `check-added-large-files` 500 KB default — losslessly optimize it
     (oxipng/pngquant); if it stays >500 KB, raise the hook's `--maxkb` with
     a comment naming the file.
2. **Accent shades are contrast-derived, not sampled.** The icon green
   `#7abf7e` is ~2.5:1 on white — it fails AA for text. Derive a dark-green
   light-theme accent (≥4.5:1 with white button text, e.g. around `#2e7d43`)
   and a light-green dark-theme accent (≥4.5:1 on `#16171d`), then verify
   every pairing computationally and record the ratios in the phase.
   - Why: US-B.2 and the UX §6 checklist make AA binding in both themes.
   - Impact: only `frontend/src/index.css` custom properties + the manifest
     `theme_color` change; components already consume the tokens.
3. **`unit` as a required column with a default, optional in requests.**
   Migration 000005: `ALTER TABLE items ADD COLUMN unit TEXT NOT NULL
   DEFAULT 'amount'` + a `CHECK (unit IN (…))` over the twelve tokens; spec
   models `Unit` as a shared enum schema, optional (default `amount`) in
   `AddItemRequest`/`UpdateItemRequest`, required on `Item`.
   - Why: existing rows and name-only quick-adds stay valid with zero
     backfill; the enum + check constraint keep API and DB agreeing.
   - Impact: `lists.Item.Unit string`, `ItemUpdate.Unit *string`, domain
     validation against the token list (single source: a `lists.Units()`
     slice the spec enum mirrors — drift caught by a test).

## Current State

```
frontend/public/icons/       placeholder solid #007aff PNGs (192/512/mask/180)
frontend/public/favicon.svg  scaffold favicon
frontend/src/assets/appicon.png  1024px source (in tree, uncommitted)
vite.config.ts               manifest theme_color #007aff
frontend/src/index.css       accent custom props (non-green), light+dark
quick-add (ListDetail.tsx)   name input only; quantity only via edit form
items table                  no unit column; Item schema has no unit
```

## Desired End State

```
quick-add:  [ Milk…            ] [− 2 +] [l ▾]  [ Add ]
row:        □ Milk        2 l          (bare "3" when unit=amount)
icons:      real artwork at 192/512/maskable/180 + favicon; theme_color green
accent:     green tokens, AA-verified light+dark
API/DB:     Item.unit (enum, default amount) end to end incl. offline payload
```

## Abstractions and Code Reuse

- `hack/generate-icons.mjs` — **new**: renders all icon sizes from the
  source (documented in the file header; re-run on artwork changes).
- `frontend/src/index.css` — accent token values only; component CSS
  untouched.
- `lists/lists.go` — `Units()` token list + `validateUnit`; `Item.Unit`,
  `ItemUpdate.Unit`.
- `splitkauf.openapi.yaml` — `Unit` enum schema referenced by `Item`,
  `AddItemRequest`, `UpdateItemRequest`.
- `frontend/src/queries.ts` / `pendingCreates.ts` (from M4) — payload gains
  `unit`; coalescing logic unchanged.

## Logging & Observability

No new logging: unit flows through existing handler/event paths; icon and
CSS changes have no runtime surface.

## Implementation

### Phase 1: US-B.1 — real app icon everywhere

Dependencies: none

**Tasks**:
- [ ] Losslessly optimize `frontend/src/assets/appicon.png`; if still
      >500 KB, add `args: [--maxkb=1024]` to `check-added-large-files` in
      `.pre-commit-config.yaml` with a comment naming the file.
- [ ] `hack/generate-icons.mjs`: render `icon-192.png`, `icon-512.png`,
      `icon-180.png` (direct resizes) and `icon-512-maskable.png` (artwork
      at ~80% composited on the icon green) into `frontend/public/icons/`;
      render a 48px `favicon.png` (and drop `favicon.svg`, updating the
      `index.html` link).
- [ ] Run it; replace the placeholder PNGs; update `frontend/index.html`
      favicon link if the filename changed.
- [ ] Test: extend the frontend build check — assert the built
      `manifest.webmanifest` still lists the three icons and the files are
      non-placeholder (size > a few KB each).

**Automated Verification**:
- [ ] `make frontend-check` and `make dist` green;
      `pre-commit run --all-files` passes with the committed source PNG.

**Manual Verification**:
- [ ] (User) Install on iOS Safari and Android: home screen shows the real
      icon, uncropped in Android mask shapes; tab shows the new favicon.

### Phase 2: US-B.2 — icon-green accent, both themes

Dependencies: none (visually reviewed together with Phase 1)

**Tasks**:
- [ ] Derive the light-theme accent (dark green, white-text ≥4.5:1) and
      dark-theme accent (light green on `#16171d` ≥4.5:1); compute and
      record every accent pairing's ratio (button text, focus ring vs bg,
      checkbox vs row bg, link vs bg) in this plan's Implementation Notes.
- [ ] `frontend/src/index.css`: swap the accent custom properties in `:root`
      and the dark block; verify checked/muted text still ≥4.5:1.
- [ ] `frontend/vite.config.ts`: manifest `theme_color` → the light accent;
      update the `index.html` `theme-color` meta if present.
- [ ] Tests: a small unit test computes the contrast of the CSS token pairs
      (parse the custom properties, assert ≥4.5:1 / ≥3:1) so regressions
      fail CI rather than an audit.

**Automated Verification**:
- [ ] `make frontend-check` green incl. the new contrast test.

**Manual Verification**:
- [ ] (User) Flip system light/dark: accent reads clearly in both; checked
      items and buttons remain legible.

### Phase 3: Migration 000005 — items.unit (own commit)

Dependencies: none

- [ ] `database/migrations/000005_item_unit.up.sql`:
      `ALTER TABLE items ADD COLUMN unit TEXT NOT NULL DEFAULT 'amount'`
      + `CHECK (unit IN ('amount','g','kg','ml','l','pack','bottle','can','jar','cup','bunch','bag'))`.
- [ ] `000005_item_unit.down.sql`: drop the column.

**Automated Verification**:
- [ ] `go build ./...`; migrate up to version 5 and down against a
      disposable Postgres.

### Phase 4: US-L.9 backend — unit through spec, domain, repo, handlers

Dependencies: Phase 3

**Tasks**:
- [ ] Spec: `Unit` enum schema (twelve tokens, default `amount`); `Item`
      gains required `unit`; `AddItemRequest`/`UpdateItemRequest` gain
      optional `unit`; `make generate`.
- [ ] `lists/lists.go`: `Units()` canonical token slice; `validateUnit`
      (empty → `amount`); `Item.Unit`, `ItemUpdate.Unit *string`; drift test
      asserting the spec enum equals `Units()` (parse the embedded spec via
      `v1.GetSwagger()`).
- [ ] `lists/service.go` + tests: unit validation on add/update; unit
      preserved through check/uncheck/restore.
- [ ] `adapters/db/lists.go` + integration tests: read/write `unit`
      everywhere items are scanned; update `AddItem`/`UpdateItem`.
- [ ] `ports/rest/v1/handlers_lists.go` + tests: map `unit` both directions;
      invalid unit → 400 validation problem (via the OpenAPI enum for free —
      add a request test pinning it).

**Automated Verification**:
- [ ] `make generate` clean; `go test ./...` + integration tests green;
      `make lint` green.

### Phase 5: US-L.9 frontend — quick-add controls and display

Dependencies: Phase 4 (and M4's Phase 5 for the payload retrofit)

**Tasks**:
- [ ] `frontend/src/api.ts`: `Unit` union type + `unit` on the item/request
      types; German display labels map (`amount` → bare number, `pack` →
      "Packung", …).
- [ ] `ListDetail.tsx` quick-add: quantity stepper (−/+, min 1, ≥44px
      targets, 8px gaps) and unit `<select>` (labeled, not
      placeholder-as-label) beside the name input; reset to 1 × amount after
      each add; focus/keyboard behavior of chained adds unchanged (existing
      test must stay green).
- [ ] Item rows + edit form: render "2 l" / "500 g" / bare amount; edit form
      gains the unit selector.
- [ ] Offline retrofit: `pendingCreates` payload and the add-item
      `mutationFn` carry `unit` (skip with a note if M4 is not yet
      implemented).
- [ ] Tests: add-with-quantity/unit round trip; bare-amount rendering;
      stepper bounds; unit preserved through the optimistic check flow.

**Automated Verification**:
- [ ] `make frontend-check` green; `make check` green from a clean tree.

**Manual Verification**:
- [ ] (User) One-handed on a phone: add "Milk, 2, l" in one gesture chain;
      row shows "2 l"; plain name-only adds feel as fast as before.

## Implementation Notes

During implementation, document user feedback, problems, and decisions here
(including the computed contrast ratios from Phase 2).

## References

- User stories: `docs/user-stories/US-B.1`, `US-B.2`, `US-L.9`.
- `frontend/src/assets/appicon.png` — icon source (green ≈ `#7abf7e`).
- `docs/agents/research/2026-07-31-mobile-first-shopping-list-ux.md` §3/§6
  (contrast, target sizes, labels, quick-add speed).
- `docs/agents/research/2026-07-21-pwa-ios-support.md` (icon/meta specifics).
- `docs/agents/plans/2026-08-01-m4-offline-first.md` (pendingCreates payload
  the unit retrofit touches).
- `docs/research/collaborative-lists.md` §1 (quantity/unit modeling).
