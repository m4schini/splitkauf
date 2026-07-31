# Research: Mobile-First UX/UI for a Collaborative Shopping List App

**Date:** 2026-07-31
**Context:** UX/UI design skills and guides for the Splitkauf PWA, focused on the core loop: **add items fast → share the list → check items off one-handed in a store aisle.**
**Method:** Web research across official design systems, UX research publications, and reference-app case studies.

---

## 1. Mobile-First Design Principles

### Thumb zones and one-handed use
- ~75% of users operate phones with one thumb (Steven Hoober's research). The screen splits into three reach zones: **easy (bottom third)** — put primary actions here (quick-add, tab bar, check-off targets); **stretch (middle)** — secondary actions; **hard (top corners)** — reserve for rarely-used items (settings, list title). The top-right corner is effectively a no-go zone for CTAs.
- In-store use is the extreme case of one-handed use: the other hand holds a basket or cart. Design as if the user *never* has two hands free.
- Support both left- and right-handed grips (10–15% of users are left-handed); avoid corner-anchored critical actions.

### Touch targets
- Minimums: **44×44pt (Apple HIG)**, **48×48dp (Material)**, and **WCAG 2.5.8 Target Size (AA)**. For an in-store check-off UI, go bigger — make the *entire list row* tappable, not just the checkbox glyph.
- Keep **≥8dp spacing** between adjacent targets; keep destructive targets physically separated from frequent ones.
- Avoid targets flush against screen edges (system gestures, phone cases).

### Bottom navigation and layout
- Bottom navigation / bottom-anchored primary actions are the 2025+ standard; top nav forces grip changes. A shopping list app arguably needs only 2–4 tabs (Lists, maybe Recipes/Profile) — don't exceed 5 (HIG).
- A persistent **bottom-anchored quick-add input or FAB** is the single most important layout decision for this app.
- Respect safe areas (home indicator, Dynamic Island); in Apple's 2025 Liquid Glass language, navigation chrome floats above content — never apply the material to content/list rows themselves.

Sources: [Parachute Design – Thumb Zone Guide](https://parachutedesign.ca/blog/thumb-zone-ux/), [Diverse – Thumb Zones 2025](https://diversewebsitedesign.com.au/designing-for-thumb-zones-mobile-ux-in-2025/), [UXPin – Touch Devices](https://www.uxpin.com/studio/blog/responsive-design-touch-devices-key-considerations/), [Upslide – One-Handed UX](https://upslidedesignstudio.com/blogs/one-handed-mobile-ux-design-best-practices-for-better-mobile-apps/), [Apple Liquid Glass docs](https://developer.apple.com/documentation/TechnologyOverviews/liquid-glass)

---

## 2. Interaction Patterns for List-Based Apps

### Quick-add input
- **Speed is the product.** Suggestions should appear from the first keystroke (<100ms perceived response); bold-highlight the matched characters.
- On mobile the keyboard covers 40–50% of the screen: show **3–5 suggestions max**, no scrolling dropdowns (Baymard found >~8 suggestions on mobile causes users to ignore them). Suggest from: user's own history/frequent items first, then a common-groceries dictionary.
- **Empty state = recents/frequents** (the "Bring! tile" insight: most shopping is repeat purchases — one-tap re-add beats typing).
- After adding, **keep the keyboard open and input focused** so users can chain multiple items ("milk ↵ eggs ↵ bread ↵").
- Never auto-commit a suggestion without an explicit tap/enter; place the input near the bottom, in thumb reach.

### Check-off, swipe, and undo
- **Tap-anywhere-on-row to check off** is the right in-store gesture (Bring! does this with tiles). Move checked items to a collapsed "done" section rather than deleting — people un-check by mistake and want to verify.
- Swipe gestures (swipe-to-delete, swipe-to-complete) are fine but must **supplement, never replace, visible controls** — provide a tap alternative for discoverability and motor-accessibility. Give clear visual feedback (revealed colored action with icon + label) as the row is dragged.
- **Undo snackbar instead of confirmation dialogs** for delete/complete. Confirmations kill the speed of the core loop; a 5s "Item deleted — Undo" toast is the pattern (soft delete under the hood).

### Optimistic UI + offline-first (critical for this app)
- Grocery stores have terrible connectivity. **Optimistic updates are mandatory**: check-offs, adds, and deletes apply instantly to local state and sync in the background; roll back (with notice) only on hard failure.
- Go **local-first**: local store is the source of truth, sync layer merges. For a shared list, the 2025 consensus is CRDTs over last-write-wins — an **OR-Set–style structure** handles "one person deletes milk while another edits it" correctly. Use a proven library (Yjs, Automerge 3.0) rather than rolling your own; for item ordering use fractional indexing. *(Note: this project's own research in `docs/research/collaborative-lists.md` evaluated CRDTs and chose per-item LWW as sufficient for the shopping-list conflict profile — treat this bullet as the broader-industry view, not a decision override.)*
- Surface sync state quietly (small "syncing/offline" indicator), never with blocking spinners. For shared lists, subtle presence/change notifications ("Alex added 3 items") build trust.

Sources: [Baymard – Autocomplete Design](https://baymard.com/blog/autocomplete-design), [Smart Interface Design Patterns – Autocomplete UX](https://smart-interface-design-patterns.com/articles/autocomplete-ux/), [uxpatterns.dev – Autocomplete](https://uxpatterns.dev/patterns/forms/autocomplete), [LogRocket – Accessible Swipe Actions](https://blog.logrocket.com/ux-design/accessible-swipe-contextual-action-triggers/), [Design Monks – Destructive Actions](https://www.designmonks.co/blog/delete-button-ui), [debugg.ai – Local-First Apps 2025](https://debugg.ai/resources/local-first-apps-2025-crdts-replication-edge-storage-offline-sync), [Hasura – Offline-First Design Guide](https://hasura.io/blog/design-guide-to-offline-first-apps), [awesome-local-first](https://github.com/alexanderop/awesome-local-first), [example CRDT grocery app](https://github.com/paulgreg/grocery-list)

---

## 3. Visual Design Guidance

### Typography
- Body text **16–17px minimum** (iOS system body is 17pt); list-item labels can be generous — in-store glanceability under motion favors larger text. Support Dynamic Type / font scaling.
- Keep the scale small: ~4–5 steps is plenty for this app (e.g., 13 caption / 15 secondary / 17 body / 22 section / 28 title). Lock **line-heights to multiples of 4** so text sits on the spacing grid.

### Spacing
- Use the **8pt grid with 4pt half-steps** (used by Material, iOS, Carbon, Fluent): spacing tokens 4, 8, 12, 16, 24, 32, 48. Typical mobile values: 16px screen-edge padding, 12–16px row padding, 8px icon-to-text gap.
- Apply the **internal ≤ external rule**: padding inside a group should be smaller than the space separating groups (makes category sections read clearly).
- List rows of 48–56dp height land cleanly on the grid *and* satisfy touch minimums.

### Color, contrast, dark mode
- WCAG 2.2 AA: **4.5:1** normal text, **3:1** large text, and **3:1 for UI components/icons** (1.4.11) — checkboxes, strikethrough "done" text, and category color chips all need checking. Don't let checked/dimmed items fall below contrast minimums.
- **Both themes must independently pass** — dark mode is not an accessibility exemption. Avoid pure white on pure black; use off-white (~#F1F1F1) on dark gray (~#121212–#1A1A1A). In dark mode, express elevation via lighter surfaces/borders, not shadows.
- Never encode meaning with color alone (e.g., category = color chip + label/icon).
- Dark mode matters for this app: evening list-making at home is a core context. Follow the system setting by default.

Sources: [Cieden – Spacing Best Practices](https://cieden.com/book/sub-atomic/spacing/spacing-best-practices), [freeCodeCamp – 8-Point Grid Typography](https://www.freecodecamp.org/news/8-point-grid-typography-on-the-web-be5dc97db6bc/), [designsystems.com – Space, Grids, Layouts](https://www.designsystems.com/space-grids-and-layouts/), [AccessibilityChecker – Dark Mode Accessibility](https://www.accessibilitychecker.org/blog/dark-mode-accessibility/), [BOIA – Dark Mode ≠ WCAG Compliance](https://www.boia.org/blog/offering-a-dark-mode-doesnt-satisfy-wcag-color-contrast-requirements), [StudioLimb – WCAG Contrast Guide](https://www.studiolimb.com/guides/wcag-color-contrast-guide.html)

---

## 4. Design Systems and Guidelines to Draw From

| System | What to take for this app |
|---|---|
| **Apple HIG** ([developer.apple.com/design](https://developer.apple.com/design/human-interface-guidelines/)) | 44pt targets; ≤5 tab bar items; safe areas; Dynamic Type; 2025 **Liquid Glass** update — translucent material for *navigation chrome only*, never list content; accessibility adaptations (Reduced Transparency/Motion, Increased Contrast) must be honored. |
| **Material 3 / M3 Expressive** ([m3.material.io/components/lists](https://m3.material.io/components/lists/overview)) | The most complete public **list-component spec**: row anatomy (leading icon/checkbox, label, supporting text, trailing action), one-/two-/three-line heights, segmented and (Dec 2025) expressive variants, selection treatment. M3 Expressive adds spring motion and shape-morph on tap/swipe (see Gmail's 2025 swipe-action redesign) — good reference for satisfying check-off animations. Also its [usability foundations](https://m3.material.io/foundations/usability) (48dp targets, contrast). |
| **NN/g** ([Checkboxes: Design Guidelines](https://www.nngroup.com/articles/checkboxes-design-guidelines/), [Easier Input on Mobile](https://www.nngroup.com/videos/mobile-input-fields/)) | Square boxes with fully clickable labels; vertical lists; checkbox = independent selection, switch = immediate on/off; labels outside the field — **never placeholder-as-label**. |
| **WCAG 2.2** ([w3.org/TR/WCAG22](https://www.w3.org/TR/WCAG22/)) | Target size 2.5.8, contrast 1.4.3/1.4.11, and gesture alternatives (2.5.1: single-pointer alternative for every swipe). |

Practical strategy: use **M3's list spec for structure/measurements**, **HIG for platform feel and ergonomics**, and check both against WCAG 2.2.

---

## 5. Reference Apps and Recommended Resources

### Reference apps — what makes each good
- **Bring!** — the category benchmark. Visual **tile grid with item illustrations** instead of a text list: adding = one tap on a recognizable tile, checking off = one tap in-store ("bingo-card" model). Frequent/recent items persist on the main screen, exploiting the fact that groceries are highly repetitive. Real-time shared lists with push notifications on changes. Known weaknesses to avoid: rigid preset categories that don't match a user's store layout, and a confusing first-run list-management flow. ([TapSmart review](https://www.tapsmart.com/apps/review-bring-shopping-lists-made-easy/), [Bring! redesign case study](https://glovorydesign.medium.com/redesign-bring-apps-shopping-list-e05c29bfcd39), [getbring.com](https://www.getbring.com/blog-posts/the-best-shopping-list-app))
- **AnyList** — benchmark for **near-real-time shared household lists**, category/aisle grouping, item notes (quantity, brand), photos on items, and recipe→list flows. Case-study critique: lacked voice input and proactive suggestions. ([anylist.com](https://www.anylist.com/), [AnyList UX case study](https://hajrahmushtaq03.medium.com/anylist-case-study-5b0df6230a6c))
- **Things 3** — benchmark for **calm visual craft** (2× Apple Design Award): restraint, no decoration without function, strong conceptual models, meaningful motion, "blank sheet of paper" feel, platform-native behavior. Not collaborative — steal its polish, not its architecture. ([Pratt design critique](https://ixd.prattsi.org/2020/02/design-critique-things-3-ios-app/), [culturedcode.com/things/features](https://culturedcode.com/things/features/))
- Also worth a look: **Todoist** (best-in-class quick-add with natural-language parsing) and **Apple Reminders** (native grocery list auto-categorization by aisle — the platform baseline you must beat).

### Key reading list
1. [Material 3 – Lists component](https://m3.material.io/components/lists/overview) — concrete row specs
2. [Apple HIG](https://developer.apple.com/design/human-interface-guidelines/) — Layout, Materials (Liquid Glass), Gestures, Accessibility
3. [Baymard – 9 Autocomplete Design Patterns](https://baymard.com/blog/autocomplete-design) — research-backed quick-add guidance
4. [LogRocket – Designing accessible swipe actions](https://blog.logrocket.com/ux-design/accessible-swipe-contextual-action-triggers/)
5. [NN/g – Checkboxes: Design Guidelines](https://www.nngroup.com/articles/checkboxes-design-guidelines/)
6. [Parachute – Thumb Zone UX guide](https://parachutedesign.ca/blog/thumb-zone-ux/)
7. [Cieden – Spacing best practices](https://cieden.com/book/sub-atomic/spacing/spacing-best-practices) + [8pt grid typography](https://www.freecodecamp.org/news/8-point-grid-typography-on-the-web-be5dc97db6bc/)
8. [Local-first apps in 2025: CRDTs and sync](https://debugg.ai/resources/local-first-apps-2025-crdts-replication-edge-storage-offline-sync) + [Hasura offline-first design guide](https://hasura.io/blog/design-guide-to-offline-first-apps)
9. [Codal – Grocery e-commerce UX (aisle-as-category)](https://codal.com/insights/5-ux-considerations-for-grocery-store-ecommerce)
10. [Dark mode accessibility](https://www.accessibilitychecker.org/blog/dark-mode-accessibility/) + [BOIA on dark mode vs WCAG](https://www.boia.org/blog/offering-a-dark-mode-doesnt-satisfy-wcag-color-contrast-requirements)

---

## 6. Actionable Do's and Don'ts Checklist

### Do
- [ ] Bottom-anchor the quick-add input/FAB; keyboard stays open for chained adds
- [ ] Suggest from user history first, from the first keystroke, max 3–5 suggestions, matched text bolded; recents/frequents shown in empty state (one-tap re-add)
- [ ] Make the **entire row** the check-off target; rows ≥48dp tall, ≥44×44pt for any sub-target
- [ ] Optimistic local updates for every action; local-first storage; quiet offline/sync indicator
- [ ] Undo snackbar (not confirmation dialogs) for delete and check-off; checked items collapse into a "done" section
- [ ] Swipe actions with visible tap alternatives, clear revealed icon+label, and haptic/animated feedback (see M3 Expressive / Gmail 2025 swipe)
- [ ] Group items by store category/aisle **and let users reorder/customize categories** (Bring!'s #1 complaint)
- [ ] 8pt spacing grid, 4pt line-height increments, 16–17px+ body text, Dynamic Type support
- [ ] AA contrast (4.5:1 text, 3:1 UI) verified in light *and* dark mode; system-following dark mode; off-white on dark gray, never pure white on black
- [ ] Show collaborator changes subtly (badge/notification: "Alex added 3 items")
- [ ] Test on real devices, one-handed, walking, in a real store

### Don't
- [ ] Don't put primary actions in the top corners or make the header interactive-heavy
- [ ] Don't require a confirmation dialog to delete or check off — speed is the product
- [ ] Don't hide any action behind gesture-only interaction (WCAG 2.5.1; discoverability)
- [ ] Don't block the UI on network — no spinners on add/check/delete, ever
- [ ] Don't use placeholder text as the input label; don't auto-commit autocomplete suggestions
- [ ] Don't hard-delete on swipe — soft delete + undo window
- [ ] Don't exceed ~5 bottom-nav tabs (this app likely needs 2–3)
- [ ] Don't let checked/dimmed item text drop below 4.5:1 contrast
- [ ] Don't rely on color alone for category meaning; pair with icons/labels
- [ ] Don't feature-creep past the core loop (recipes, meal plans, pantry inventory) before add/check/share is flawless — the common thread of Bring!, AnyList, and Things 3 is ruthless optimization of one loop

---

**One-paragraph design brief distilled from all of the above:** Build a bottom-anchored, keyboard-persistent quick-add with history-first suggestions; a category-grouped list of tall, full-row-tappable items that check off instantly with a satisfying animation and undo toast; offline-first sync so two people in different aisles never lose an edit; M3 list anatomy with HIG ergonomics; 8pt grid, ≥17px text, AA contrast in both themes; and Things-3-level restraint everywhere else.
