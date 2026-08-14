---
date: 2026-08-14T17:13:10+00:00
git_commit: 3ede5ea345071f00c5366bd18e51552e8a71c45f
branch: main
topic: "Icon buttons for list and item actions"
tags: [plan, frontend, lists, items, accessibility]
status: ready
---

# PLAN: Icon buttons for list and item actions

Replace the text labels on the list/item action buttons (Back, Copy, Rename, Delete list, Edit item, Remove item) with locally bundled SVG icons, following accessibility best practices. Icons must never trigger an external/network call at runtime.

## Acceptance Criteria

- List/item action buttons (Back, Copy, Rename, Delete list on both screens, Edit item, Remove item) render an icon instead of text.
- Every icon-only button keeps its descriptive `aria-label`; the SVG itself is hidden from assistive tech via an explicit `aria-hidden="true"` prop (lucide-react does NOT set this by default).
- Icons are bundled locally via `lucide-react` — no runtime network requests for icon assets.
- Touch targets stay ≥44×44px; danger buttons stay visually distinct (red icon via `currentColor`); the global `:focus-visible` ring applies unchanged.
- Add, Save, Cancel, Undo, and the −/+ stepper keep their current text/symbols.
- Existing tests (which query buttons by accessible name) keep passing.

## Technical Key Decisions and Tradeoffs

1. **Icon source: `lucide-react` npm package:** add as a regular dependency.
   - Why: MIT-licensed, professionally maintained, tree-shaken ES modules — only the imported icons land in the bundle, served from the app's own origin like any other JS. No runtime fetches.
   - Impact: one new dependency in `frontend/package.json`; icons are plain React components (`<Trash2 />`).
2. **Icon-only for row/header actions, text kept elsewhere:** Back, Copy, Rename, Delete, Edit, Remove become icon-only; Add, Save, Cancel, Undo, stepper stay textual.
   - Why: these six actions map to universally understood icons and already carry descriptive `aria-label`s; primary/form actions are clearer with text (icon-only "Save"/"Add" is an accessibility trade-off with no space benefit there).
   - Impact: only `ListsOverview.tsx` and `ListDetail.tsx` change; the quick-add form, edit form, and snackbar are untouched.
3. **Icon mapping:** Back → `ArrowLeft`, Copy → `Copy`, Rename → `Pencil`, Delete list → `Trash2`, Edit item → `Pencil`, Remove item → `Trash2`.
   - Why: conventional, instantly recognizable glyphs; pencil/trash reuse across contexts (header vs. row) is intentional and standard practice.
   - Impact: pencil and trash appear in two places each — the `aria-label`s disambiguate for assistive tech, position disambiguates visually.
4. **CSS: `.icon-button` becomes a centered square:** padding `8px 16px` → `8px`, add `display: inline-flex; align-items: center; justify-content: center`, keep `min-width/min-height: 44px`; icons render at 20px and inherit `currentColor` (so `.icon-button.danger` turns the trash icon red for free).
   - Why: text-width padding makes icon-only buttons lopsided; `currentColor` reuses the existing danger styling with zero extra CSS.
   - Impact: one CSS block change; all six buttons pick it up automatically.

## Current State

React 19 + Vite PWA frontend with no icon library. Buttons use the `.icon-button` class (`frontend/src/index.css:317`) but render text:

```
ListsOverview (Your lists)                ListDetail (one list)
┌────────────────────────────┐            ┌──────────────────────────────────┐
│ Your lists                 │            │ [Back] Groceries                 │
│                            │            │        [Copy] [Rename] [Delete]  │
│ ┌────────────────────────┐ │            │ Open (2)                         │
│ │ Groceries      [Delete]│ │            │ ☐ Milk 2 l      [Edit] [Remove]  │
│ │ 3 open · by you        │ │            │ ☐ Eggs          [Edit] [Remove]  │
│ └────────────────────────┘ │            │ Done (1)                         │
│                            │            │ ☑ Bread         [Edit] [Remove]  │
│ [input........] [Add]      │            │ [input........] [Add]            │
└────────────────────────────┘            │ [−] 1 [+]  Unit [Stück ▾]        │
                                          └──────────────────────────────────┘
```

(The ListDetail header is a single `screen-header` flex row in the markup; the text buttons merely wrap on narrow screens. Iconifying them makes it fit one line.)

Button inventory (all already have `aria-label`s and ≥44×44px targets):

- `frontend/src/ListsOverview.tsx:100-107` — per-row **Delete** (`icon-button danger`, `aria-label="Delete list ${name}"`)
- `frontend/src/ListDetail.tsx:388-390` — header **Back** (`aria-label="Back to lists"`)
- `frontend/src/ListDetail.tsx:407-431` — header **Copy**, **Rename**, **Delete** (`aria-label`s "Copy list", "Rename list", "Delete list")
- `frontend/src/ListDetail.tsx:176-199` — per-item **Edit**, **Remove** (`aria-label`s "Edit ${name}", "Remove ${name}")

A global `:focus-visible` outline exists at `frontend/src/index.css:77`. Tests (`ListsOverview.test.tsx`, `ListDetail.test.tsx`, `App.test.tsx`, `offlineOutbox.test.tsx`) locate these buttons by accessible name, which the `aria-label`s will continue to provide.

## Desired End State

```
ListsOverview (Your lists)                ListDetail (one list)
┌────────────────────────────┐            ┌──────────────────────────────────┐
│ Your lists                 │            │ [←] Groceries      [⧉] [✎] [🗑]  │
│                            │            │ Open (2)                         │
│ ┌────────────────────────┐ │            │ ☐ Milk 2 l              [✎] [🗑] │
│ │ Groceries         [🗑] │ │            │ ☐ Eggs                  [✎] [🗑] │
│ │ 3 open · by you        │ │            │ Done (1)                         │
│ └────────────────────────┘ │            │ ☑ Bread                 [✎] [🗑] │
│                            │            │ [input........] [Add]            │
│ [input........] [Add]      │            │ [−] 1 [+]  Unit [Stück ▾]        │
└────────────────────────────┘            └──────────────────────────────────┘
```

Each `[icon]` is a 44×44px button showing a 20px lucide SVG (`aria-hidden` on the SVG, `aria-label` on the button). Danger buttons show a red trash icon. Everything else is unchanged.

## Abstractions and Code Reuse

No new abstractions. Lucide icons are imported directly where used; the existing `.icon-button` class is reused as the single styling hook.

- `frontend/`
  - `package.json` — add `lucide-react` to `dependencies`
  - `src/index.css` — `.icon-button` block: square padding + flex centering; new `.icon-button svg` size rule
  - `src/ListsOverview.tsx` — import `Trash2`; per-row Delete button renders `<Trash2 />` instead of "Delete"
  - `src/ListDetail.tsx` — import `ArrowLeft, Copy, Pencil, Trash2`; header and `ItemRow` buttons render icons instead of text

## Logging & Observability

None — purely presentational frontend change.

## Implementation

### Phase 1: Dependency, CSS, and ListsOverview delete button

Dependencies: None

Add `lucide-react`, restyle `.icon-button` for icon-only content, and convert the first button (per-row Delete on the overview) as the vertical slice proving the pattern.

**Tasks**:
- [x] Run `npm install lucide-react` in `frontend/` (adds to `dependencies` in `package.json` and updates `package-lock.json`)
- [x] In `frontend/src/index.css`, update the `.icon-button` block (line 317) for icon-only content:
  ```css
  .icon-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 8px;
    background: none;
    border: 1px solid var(--control-border);
    color: var(--fg);
  }
  ```
  (min-width/min-height/border-radius/font-size/cursor continue to come from the shared block at line 305.)
- [x] In `frontend/src/ListsOverview.tsx`, import `Trash2` from `lucide-react` and replace the "Delete" text child of the per-row delete button (line 100) with `<Trash2 size={20} aria-hidden="true" />`, keeping `className="icon-button danger"` and the `aria-label` unchanged. Note: lucide-react does NOT set `aria-hidden` itself — pass it explicitly on every icon.
- [x] In `frontend/src/ListsOverview.test.tsx`, add an assertion that the delete button's accessible name is intact and it contains no visible text (icon-only): query `getByRole('button', { name: 'Delete list Groceries' })` (the existing fixture list name) and assert `button.textContent === ''`

**Automated Verification**:
- [x] `make frontend-check` passes (runs lint, format-check, typecheck, and the vitest suites — existing tests query by accessible name and must stay green, plus the new icon-only assertion)

### Phase 2: ListDetail header and item-row buttons

Dependencies: Phase 1

Convert the remaining five buttons on the list detail screen to icons.

**Tasks**:
- [ ] In `frontend/src/ListDetail.tsx`, import `ArrowLeft, Copy, Pencil, Trash2` from `lucide-react`
- [ ] Replace the header button text children, keeping classes, `aria-label`s, and handlers unchanged (all icons get `aria-hidden="true"`): Back (line 388) → `<ArrowLeft size={20} aria-hidden="true" />`, Copy (line 407) → `<Copy size={20} aria-hidden="true" />`, Rename (line 416) → `<Pencil size={20} aria-hidden="true" />`, Delete (line 424) → `<Trash2 size={20} aria-hidden="true" />`
- [ ] In `ItemRow` (same file), replace the action button text children: Edit (line 176) → `<Pencil size={20} aria-hidden="true" />`, Remove (line 188) → `<Trash2 size={20} aria-hidden="true" />`
- [ ] In `frontend/src/ListDetail.test.tsx`, add an assertion mirroring Phase 1's: the header buttons and one item row's Edit/Remove buttons are reachable by accessible name and have empty `textContent`

**Automated Verification**:
- [ ] `make frontend-check` passes
- [ ] `cd frontend && npm run build` succeeds, and the bundle contains no third-party icon URLs: `grep -REn "https?://[^\"']*(lucide|unpkg|jsdelivr|cdn\.)" ../ports/web/dist/assets/*.js` returns nothing (note: the build empties and repopulates `ports/web/dist`, which the Go `ports/web` layer embeds — leave the fresh build output in place)

**Manual Verification**:
- [ ] Open the app: overview rows show a red trash icon; list detail shows ←, copy, pencil, trash in the header and pencil/trash per item row
- [ ] Keyboard-tab through the buttons: the green focus ring is visible on each icon button
- [ ] With VoiceOver (or the browser accessibility inspector), each icon button announces its full label ("Delete list Groceries", "Back to lists", "Edit Milk", …)
- [ ] With devtools network tab open, reload and click through both screens: no network requests for icon/font/SVG assets from third-party origins

## Implementation Notes

During implementation, document user feedback, problems, and decisions here.

## References

- `frontend/src/ListsOverview.tsx`, `frontend/src/ListDetail.tsx` — the only components changing
- `frontend/src/index.css:303-330` — shared button sizing and `.icon-button` styling
- `docs/agents/research/2026-07-22-friendly-mobile-ui-design.md` — ≥44px target / visible-action guidance this plan preserves
- lucide-react: https://lucide.dev/guide/packages/lucide-react (MIT; note it does NOT set `aria-hidden` on its SVGs — the plan passes it explicitly)
