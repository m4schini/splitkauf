# Research: Friendly/Playful Mobile-First UI Design for a Shopping-List PWA

**Date:** 2026-07-22
**Context:** Design research for a React PWA (installable on iOS) for collaborative household shopping lists.
**Method:** Multi-angle web search + adversarial claim verification. Confidence notes are explicit per section.

---

## 1. Visual Language for Friendly Utility Apps

### 1.1 Color Palette Approaches

**Verified:** The 2025–2026 consensus for friendly utility apps is a **hybrid approach**: pastel/soft base color + one vibrant accent (e.g., soft ivory background + electric mint CTA). Pastels dominate for apps used in extended daily sessions because they reduce eye fatigue and promote focus. Vibrant-only palettes suit entertainment or edgy fintech (e.g., Cleo).

The **60-30-10 rule** is consistently cited: 60% neutral background, 30% secondary/surface color, 10% accent.

**Trending 2025 pastel palette options:**
- Butter Yellow — friendly/optimistic; utility apps
- Blush Pink (~#F5A0B5) — warm/social
- Lavender (~#C3A4E0) — productivity
- Mint Green (~#BDF7E5) — finance/fresh
- Sage, Terracotta, Sand — earth tones (warm neutrals)
- Pantone 2025 Color of the Year: **Mocha Mousse** (warm brown) — suggested for nav bars / CTAs

**Utility-app palette patterns (verified via design case studies):**
- Classic: `#0A2540` navy + `#00D4B2` mint + white
- Mint+Teal: `#00BFA5` primary, `#BDF7E5` secondary, `#FF9F8A` coral accent
- Role convention (multiple sources): teal/violet = primary accent; coral/yellow = CTA

Sources: [Envato color trends](https://elements.envato.com/learn/color-scheme-trends-in-mobile-app-design), [mockflow.com color psychology](https://mockflow.com/blog/color-psychology-in-ui-design)

---

### 1.2 Rounded Corners

**Verified:** Both major platforms converged on roundness in 2025:

- **Material 3 Expressive** (Google I/O, May 2025): Added 35 new shape variants, a 10-step radius scale with named tokens, shape morphing animations, and a rounded typeface (Google Sans Flex Rounded).
- **Apple Liquid Glass** (iOS 26, 2025): Translucent rounded components as the new system default.

Pill buttons and soft-cornered cards are now the baseline "friendly" register — not a differentiator.

**Practical radius values (verified via M3 spec + design system blogs):**
- M3 named tokens: 4 / 8 / 12 / 16 / 20 / 28 / 32 / 48 dp + full pill (`9999px`)
- Cards: 12 dp (M3 default), 12–16 px (community consensus for "premium" feel)
- Modals / side sheets: 16 dp
- Large containers: 20–24 px
- Rule of thumb: radius ≈ 5–10% of element's smallest side
- Nested inner radius formula: `inner_radius = outer_radius - padding`
- `corner-shape: squircle` (CSS proposal, gaining traction) — superellipse shape, not yet widely implemented

Sources: [m3.material.io shape](https://m3.material.io/styles/shape/corner-radius-scale), [Material 3 Expressive announcement](https://supercharge.design/blog/material-3-expressive), [border-radius rules](https://blog.92learns.com/border-radius-rules/)

---

### 1.3 Illustration and Mascot Use

**Verified use cases (multiple sources):**
- **Onboarding:** 3–5 illustrated screens, target completion under 60 seconds
- **Empty states:** Illustration + contextual CTA replaces "No data" — significantly improves perceived quality
- **Milestones/achievements:** Confetti, victory poses, celebration art on milestone events (e.g., list completed)
- **Loading screens:** Short branded Lottie animations

**Mascot benchmarks (verified):**
- **Duolingo** (52.7M DAUs Q4 2025): 34% DAU lift attributed to refining the Duo owl's interaction strategy. Shows mascots drive retention in utility apps with repetitive tasks.
- **Toshl Finance** (3M+ users, NYT-cited): Playful monster mascots throughout the app. The canonical finance-mascot success story.
- Industry rule: Only ~16% of mascots qualify as distinctive assets (Ipsos/JKR research). Strong mascots simplify as they scale — detail is removed, not added.
- **Dropbox**: Most-cited case study for custom illustration driving "approachable" brand perception.

**Recommendation pattern:** Mascots/illustrations are most effective at onboarding, empty states, and milestone celebrations. Using them in transactional flows (item entry, list views) typically adds noise.

Sources: [Tubik mascot blog](https://tubikstudio.com/blog/design-me-live-the-power-of-mascots-in-ui-and-branding/), [Duolingo design breakdown](https://www.925studios.co/blog/duolingo-design-breakdown), [raw.studio mascots](https://raw.studio/blog/how-mascots-improve-user-experience/), [Toshl Finance](https://www.fusioncharts.com/blog/personal-finance-apps-with-amazing-dashboards-part-2-toshl-finance/)

---

### 1.4 Typography Choices

**Verified:** Rounded sans-serif is the dominant register for approachable utility apps.

| Font | Notes | Best Use |
|---|---|---|
| **Figtree** | Erik Kennedy pick; more characterful than Poppins; loads ~33ms faster; near-identical metrics to Poppins (swap-compatible) | Headings + body; modern workhorse |
| **Nunito / Nunito Sans** | Rounded terminals, warm feel | Body copy; warm secondary |
| **Poppins** | Geometric, 8+ weights; slightly colder | Display headings; established "startup" feel |
| **Lato** | Humanist, safe | Body copy; legibility-first |
| **Inter** | Safest cross-platform | System-default feel; utility |
| **Circular** | Spotify/Airbnb; proprietary cost | Premium but expensive |

**2026 trend (inferred from design press):** "Bouba Grotesks" — soft humanist neo-grotesks with gently rounded terminals, described as "almost smiling" letterforms. Figtree and Nunito fit this archetype.

**Recommended pairing:** Figtree 700–800 for headings / 600 for subheads + Nunito 400 body / 600–700 buttons. Caution: avoid pairing two geometric sans at the same hierarchy level.

Type scale: Body 14–18 px, headings 24–36 px.

Sources: [frontmatter.io fonts 2025](https://www.frontmatter.io/blog/best-fonts-for-apps-in-2025-top-picks-for-ios-and-android-ui-design), [Poppins vs Figtree](https://fontdiff.com/compare/poppins-vs-figtree/), [rounded fonts 2026](https://graphicdesignjunction.com/2026/05/30-top-rounded-fonts-for-2026/)

---

### 1.5 Dark Mode Considerations

**Verified design rules:**

1. **Never invert light colors directly.** Saturated coral → soft rose/peach in dark mode; bright red errors → muted pastel rose. Formula: lighten accents ~+25% and desaturate ~−10% for dark surfaces in HSL.

2. **Background color:** Avoid pure `#000000` (halation on OLED). Standards:
   - Material default: `#121212`
   - Alternatives: `#181A1B`, `#1E1E1E`, `#23272F`
   - Tinted blacks (e.g., navy `#14213D`) read friendlier than neutral black for "warm" apps

3. **Elevation via luminance, not shadows:** Layer brightening ~3–8% per step: `#121212` canvas → `#1A1A1A` → `#22–#2A` cards → `#2D–#38` modals. Material You tints elevated layers toward brand color.

4. **Text:** `#F5F5F5` / `#E0E0E0` rather than pure white. Google opacity hierarchy: 87% / 60% / 38% white for primary / secondary / disabled.

5. **2026 trend (verified from design press):** "Soft-tech pastels" — desaturated pastel accents + soft gradients over charcoal bases.

**Market reality:** 70–82% of users enable dark mode — it is a baseline expectation, not optional.

Sources: [mindinventory dark mode](https://www.mindinventory.com/blog/how-to-design-dark-mode-for-mobile-apps/), [muz.li dark mode systems](https://muz.li/blog/dark-mode-design-systems-a-complete-guide-to-patterns-tokens-and-hierarchy/)

---

## 2. Mobile Touch Interaction Standards

### 2.1 Minimum Touch Target Sizes

**Verified via official specs:**

| Standard | Minimum Touch Target | Notes |
|---|---|---|
| Apple HIG | 44 × 44 pt = **44 CSS px** | Visible element may be smaller; hit area must meet minimum |
| Material Design 3 | 48 × 48 dp ≈ **9 mm physically** | Visual element can be 24 × 24 dp; transparent padding fills the rest |
| WCAG 2.5.5 (AAA) | 44 × 44 CSS px | Non-binding but best practice |
| WCAG 2.5.8 (AA, WCAG 2.2) | 24 × 24 CSS px minimum | Binding for accessibility compliance |

**Cross-platform safe minimum: 48 px.** This satisfies both Apple HIG and Material Design 3.

Targets must be separated by at least **8 dp of space** (MD3).

Sources: [Apple design tips](https://developer.apple.com/design/tips), [m2.material.io touch target](https://m2.material.io/develop/web/supporting/touch-target), [mobileviewer.io](https://www.mobileviewer.io/blog/touch-target-size)

---

### 2.2 Thumb Zone / Bottom Reachability

**Verified (Steven Hoober research, repeatedly cited):**
- 49% of users navigate with one thumb
- Three zones: **Green (easy reach)** = bottom-center arc; **Yellow (stretch)** = mid-screen sides; **Red (difficult)** = top corners
- Center bottom tab is ergonomically optimal on large phones
- Top-right (hamburger menus, search icons) is the hardest-to-reach zone — avoid for frequent actions
- **Destructive actions should intentionally go in harder-to-reach zones** (yellow/red) as a safety UX pattern

**Foldables (inferred):** Two-thumb use when unfolded, with a different hard zone (upper-center).

Sources: [diversewebsitedesign thumb zones](https://diversewebsitedesign.com.au/designing-for-thumb-zones-mobile-ux-in-2025/), [timgraf.com thumb zone guide](https://timgraf.com/ux-design/designing-for-the-thumb-zone-a-modern-guide-to-mobile-ux-that-respects-human-anatomy/)

---

### 2.3 Navigation: Bottom Tab Bar vs. Hamburger Menu

**Verified consensus (2025–2026): Bottom tab bar is the primary recommendation.**

| Factor | Bottom Tab Bar | Hamburger Menu |
|---|---|---|
| Discoverability | High | Low |
| Thumb ergonomics | Excellent | Moderate |
| Best for # sections | 3–5 | 6+ |
| Platform recommendation | Apple HIG mandates it; Google recommends it | Secondary navigation, overflow |

- Airbnb measured **40% faster task completion** with bottom tabs vs. hamburger menu.
- **2025–2026 trend:** Hybrid navigation — persistent bottom tab bar for 3–5 primary sections + slide-out drawer for secondary/overflow items (Instagram, Spotify pattern).
- Hamburger menus are valid for secondary navigation or apps where section-switching is rare. Not dead — just demoted.

Sources: [acclaim.agency navigation patterns](https://acclaim.agency/blog/the-future-of-mobile-navigation-hamburger-menus-vs-tab-bars), [nngroup.com mobile navigation](https://www.nngroup.com/articles/mobile-navigation-patterns/), [phone-simulator.com 2026](https://phone-simulator.com/blog/mobile-navigation-patterns-in-2026)

---

### 2.4 FAB (Floating Action Button) Placement

**Verified via Material Design 3 guidelines (updated May 2025):**

- **Default position:** Bottom-right (favors right-handed majority)
- **Inclusive alternative:** Bottom-center (avoids left-hand discrimination; risks obscuring content)
- **One FAB per screen only.** Never per-card or per-list-item.
- FAB = positive primary actions (Create, Share, Add). Never destructive actions.
- On compact screens: FAB sits above the navigation bar.
- On medium/expanded screens (tablet/desktop): FAB moves to the **top of the navigation rail** (leading edge).

**FAB size variants (May 2025 update):**
- Regular: 56 dp
- Medium: 80 dp
- Large: 96 dp
- **Small (40 dp): Deprecated as of May 2025** — do not use
- New: **FAB Menus** (expand to show 2–6 related actions) replace the old speed dial pattern

Sources: [m3.material.io FAB guidelines](https://m3.material.io/components/floating-action-button/guidelines), [boltuix.com FAB 2025](https://www.boltuix.com/2025/06/materialfab.html)

---

### 2.5 Swipe Gestures on List Items

**Verified patterns:**

- Swipe gestures are ~5× faster than finding and tapping a button (~100 ms vs ~500 ms)
- **Progressive reveal is preferred over immediate destructive action**: swipe to reveal action buttons, not swipe to instantly delete
- **Left-swipe = destructive/delete** is near-universal convention (from iOS Mail). Maintain consistency throughout the app.
- Always provide **undo / soft-delete recovery** — accidental swipes are common
- Swipe is not self-discoverable: require visual affordances (hint animation on first use)
- Provide visual or haptic feedback when gesture threshold is crossed
- **Gesture conflicts:** never use horizontal swipe on items inside a horizontal scroll container
- **Accessibility:** always provide a tap alternative for all swipe actions (long-press menu or three-dot menu)

Sources: [logrocket swipe UX](https://blog.logrocket.com/ux-design/accessible-swipe-contextual-action-triggers/), [nngroup contextual swipe](https://www.nngroup.com/articles/contextual-swipe/)

---

### 2.6 Pull-to-Refresh

**Verified patterns:**

- Apply only to **feeds, inboxes, and frequently-changing lists** — not static configuration screens
- Gesture: pull down past a threshold → release → spinner → content updates
- Spinner must remain active until data is actually loaded (not just until the request fires)
- Gesture responsiveness is critical: sluggish pull destroys perceived quality
- Avoid on iPad landscape (ergonomically awkward)
- Avoid conflict with bottom sheet dismiss gestures
- Pull-to-refresh animations are a branding opportunity (small Lottie animation = delight without cost)

Sources: [bizcatalyst360.com pull-to-refresh](https://www.bizcatalyst360.com/mastering-pull-to-refresh-design-principles-and-best-practices/), [uxplanet pull-to-refresh](https://uxplanet.org/pull-to-refresh-ui-pattern-42a85f671cdf)

---

### 2.7 Bottom Sheets vs. Modals

**Verified breakdown:**

| | Non-modal Bottom Sheet | Modal Bottom Sheet | Full-Screen Modal |
|---|---|---|---|
| Context preserved | Yes | Partial | No |
| Interruption level | Low | Medium | High |
| Background overlay | No | Yes | Yes |
| Background interaction | Allowed | Blocked | Blocked |
| Best for | Paired info (maps, detail) | Quick focused actions | Multi-step complex tasks |

- Bottom sheets achieve **25–30% higher engagement** than traditional center modals (less intrusive, easier to dismiss).
- Bottom sheets are better for **context preservation** than purely ergonomics.
- **Common mistake:** Large modal backdrops that dismiss on tap → accidental data loss. Always confirm or auto-save before dismissing.
- Limit to **one modal interruption per session** where possible; a second interruption multiplies friction.

Sources: [nngroup.com bottom sheet](https://www.nngroup.com/articles/bottom-sheet/), [logrocket bottom sheets](https://blog.logrocket.com/ux-design/bottom-sheets-optimized-ux/), [digia.tech modals](https://www.digia.tech/post/bottom-sheets-vs-modals-interruption-layer/)

---

### 2.8 Haptic Feedback on Web / PWA (iOS Safari) — CRITICAL

**Verified: `navigator.vibrate` does not work on iOS.**

| Platform | `navigator.vibrate` Support |
|---|---|
| Chrome for Android (30+) | Yes |
| Edge (79+) | Yes |
| Samsung Internet | Yes |
| iOS Safari (any version) | **Never implemented** |
| Firefox 129+ (June 2024) | **Removed** |
| PWA on iOS (workaround) | Partial — see below |

**W3C status (verified):** The W3C formally proposed marking the Vibration API as an **Obsolete Recommendation** (September 2024), citing privacy/fingerprinting risks and better native alternatives. The API's future is uncertain even on Android.

**iOS workaround history:**
- iOS 17.4+ added `<input type="checkbox" switch>` — a non-standard attribute where toggling fires the system Taptic Engine
- Libraries (`ios-vibrator-pro-max`, `ios-haptics`) exploited this by programmatically `.click()`-ing a hidden switch label
- **Patched in iOS 26.5 (May 2026):** Programmatic `.click()` no longer triggers the Taptic Engine
- **What still works:** An invisible `<input type="checkbox" switch>` overlaid directly under the user's finger (opacity: 0), so the *user's direct touch* toggles it. Direct user interaction still fires the Taptic Engine.

**Design conclusion:** Treat all web haptics as **progressive enhancement**. Design interactions to feel complete without haptics. The invisible-overlay workaround is brittle and accessibility-hostile.

Sources: [vibrator.dev](https://vibrator.dev/), [ios-vibrator-pro-max GitHub](https://github.com/samdenty/ios-vibrator-pro-max), [W3C Obsolete proposal](https://lists.w3.org/Archives/Public/public-review-announce/2024Sep/0003.html), [progressier.com vibration](https://progressier.com/pwa-capabilities/vibration-api), [testmuai.com vibration](https://www.testmuai.com/learning-hub/vibration-api-browser-support/)

---

## 3. Micro-interactions and Celebration Patterns

### 3.1 React Libraries — Current Status (July 2026)

| Library | npm Package | Latest Version | Status | Notes |
|---|---|---|---|---|
| Confetti (framework-agnostic) | `canvas-confetti` | 1.9.4 | Active (7.5M downloads/week) | "Key ecosystem project", rated Sustainable |
| Confetti (React) | `react-confetti` | 6.4.0 | Active | More adopted than canvas wrapper |
| Confetti (React boom) | `react-confetti-boom` | — | Active | Two modes: `'boom'` and `'fall'` |
| Motion (ex-Framer Motion) | `motion` (import from `motion/react`) | v12 | Active (30M downloads/month) | `framer-motion` still on npm but no longer actively developed |
| Lottie (React, recommended) | `@lottiefiles/dotlottie-react` | 0.19.4 | Active | Rust+WASM core, ~100KB vs 500KB legacy |
| Lottie (deprecated) | `@lottiefiles/react-lottie-player` | — | **Deprecated** | Do not use |
| React Spring | `@react-spring/web` | 10.1.1 | Active | Physics-based, no re-render overhead |
| AutoAnimate | `@formkit/auto-animate` | — | Active | Zero-config FLIP list animations; one line |

**Framer Motion → Motion migration (verified):**
- `framer-motion` rebranded to `motion` as an independent project
- Import path changes from `framer-motion` to `motion/react`
- API is identical — migration is a one-line import swap
- Motion v12 uses Web Animations API + ScrollTimeline for 120fps hardware-accelerated animations
- Falls back to JS for spring physics and custom gestures

**Lottie recommendation (verified):**
- `@lottiefiles/dotlottie-react` is the current recommended package
- Uses `.lottie` format (bundled, supports themes and state machines)
- ~100KB vs `lottie-web`'s ~500KB — meaningful for PWA bundle size
- One fintech shipped 12 celebration animations at 400 KB total

Sources: [canvas-confetti npm](https://www.npmjs.com/package/canvas-confetti), [react-confetti npm](https://www.npmjs.com/package/react-confetti), [motion.dev](https://motion.dev/), [Motion upgrade guide](https://motion.dev/docs/react-upgrade-guide), [@lottiefiles/dotlottie-react npm](https://www.npmjs.com/package/@lottiefiles/dotlottie-react), [react-spring.dev](https://www.react-spring.dev/)

---

### 3.2 Celebration Pattern Best Practices

**Verified trend (2025):** Purposeful, **sparse** celebration rather than overwhelming effects.

**Pattern for the "list completed" moment (inferred from multiple sources):**
1. Clean animated checkmark (confident tick, ~300ms)
2. Subtle color shift to success green
3. Short confirmation message ("All done!", "Shopping complete")
4. Optional: single confetti burst (not a 3-second fireworks show)
5. Optional: soft chime sound (only if user hasn't disabled sound)

**Anti-pattern:** Repeated confetti for routine actions feels mocking. Reserve confetti for: first-time milestones, completing a whole list.

**Progress rings / animated checkmarks:** Use `motion` (Framer Motion) `<motion.path>` with `pathLength` animation for SVG checkmarks — this is the standard React pattern. No dedicated library needed.

Sources: [motion.dev confetti example](https://motion.dev/examples/react-confetti), [MagicUI confetti](https://magicui.design/docs/components/confetti), [medium micro-interactions](https://medium.com/@ryan.almeida86/5-micro-interactions-to-make-any-product-feel-premium-68e3b3eae3bf)

---

## 4. PWA-Specific Mobile UI Constraints on iOS

### 4.1 Safe Areas (`env(safe-area-inset-*)`)

**Verified:** Safe area insets work correctly on iPhone (iOS 11.2+). Required setup:

```html
<!-- In <head> — required for env() to have non-zero values -->
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
```

```css
/* Use CSS custom properties for reusability */
:root {
  --safe-top: env(safe-area-inset-top, 0px);
  --safe-bottom: env(safe-area-inset-bottom, 0px);
  --safe-left: env(safe-area-inset-left, 0px);
  --safe-right: env(safe-area-inset-right, 0px);
}

/* Scope to standalone mode to avoid affecting browser view */
@media (display-mode: standalone) {
  .app-header { padding-top: var(--safe-top); }
  .bottom-nav { padding-bottom: var(--safe-bottom); }
}
```

**Dynamic Island:** Handled via `env(safe-area-inset-top)` — value accounts for Dynamic Island height on Pro models.

**Known bug (verified):** `env(safe-area-inset-bottom)` drops to `0px` in Next.js when navigating via `<Link>` from `next/link`. Normal `<a>` tags preserve the `34px` value. Workaround: force CSS recompute or use `useLayoutEffect`.

**iPadOS 26 regression (verified from dev reports):** `env(safe-area-inset-*)` does not work correctly in iPadOS 26's new windowed mode with "traffic light" window controls. iPhone targets are unaffected.

**Bottom inset value:** Typically `34px` on iPhone with home indicator (no physical Home button).

Sources: [magicbell.com PWA iOS guide](https://www.magicbell.com/blog/pwa-ios-limitations-safari-support-complete-guide), [web.dev app-design](https://web.dev/learn/pwa/app-design), [Next.js safe-area bug](https://github.com/vercel/next.js/discussions/81264), [iPadOS 26 PWA report](https://dev.to/reinhart1010/pwa-in-ipados-26-is-a-joke-38g1)

---

### 4.2 Standalone Display Mode

**Verified behaviors:**
- Safari URL bar and browser chrome are hidden
- System status bar remains visible (time, battery, signal)
- No browser back button / back gesture available — **must implement all navigation within the app**
- Use `@media (display-mode: standalone)` to detect and adjust layout
- Opening external links in standalone PWA: they open in a new Safari instance (breaks the app feel) — use `target="_self"` or handle in-app with a WebView component

**Installation friction (verified):** iOS PWA install is a manual 4+ step process hidden in Safari Share menu ("Add to Home Screen"). No `beforeinstallprompt` event on iOS — cannot trigger a custom install prompt. Must educate users with an in-app banner.

Sources: [brainhub.eu PWA iOS status](https://brainhub.eu/library/pwa-on-ios), [vinova.sg PWA Safari tips](https://vinova.sg/navigating-safari-ios-pwa-limitations/)

---

### 4.3 Viewport Height: `100vh` Bug and `dvh`/`svh`

**Verified — this is a real and current issue.**

| Unit | Behavior | Recommendation |
|---|---|---|
| `100vh` | Equals *large* viewport (address bar hidden) — content clips behind visible toolbar | Avoid for full-screen layouts |
| `100dvh` | Tracks real-time visible area | Causes layout reflow/jank as iOS address bar animates |
| `100svh` | Fixed to *smallest* viewport (address bar visible) | Most stable; may show extra space when scrolled |
| `100lvh` | Fixed to *largest* viewport | Same as `100vh` — same bug |

**Safari support (verified):** `dvh`, `svh`, `lvh` supported in **Safari 15.4+** (iOS 15.4+, released March 2022). Safe to use for modern targets.

**Keyboard interaction note (verified):** On Android, when the soft keyboard opens, `dvh` shrinks to the area above the keyboard, causing modals to jump. Use `100svh` for modal height or use `window.visualViewport` listener.

**Recommended production pattern:**

```css
.full-screen-container {
  min-height: 100svh; /* Stable floor */
  height: 100dvh;     /* Dynamic fill */
}
```

Sources: [web.dev viewport units](https://web.dev/blog/viewport-units), [caniuse viewport-unit-variants](https://caniuse.com/viewport-unit-variants), [medium dvh guide](https://medium.com/@tharunbalaji110/understanding-mobile-viewport-units-a-complete-guide-to-svh-lvh-and-dvh-0c905d96e21a)

---

### 4.4 Tap Highlight Color

**Verified:** `-webkit-tap-highlight-color` is a non-standard WebKit CSS property that shows a semi-transparent highlight when users tap interactive elements. The default is a gray/black overlay.

**Standard reset (used in most CSS resets and Tailwind base styles):**

```css
* {
  -webkit-tap-highlight-color: transparent;
}
```

**Accessibility warning:** Removing the tap highlight without providing an alternative `:active` state creates a dead-feeling interface. Always add a custom `:active` style:

```css
button:active, a:active {
  opacity: 0.7; /* or scale: 0.97, or background color change */
}
```

Sources: [MDN -webkit-tap-highlight-color](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/-webkit-tap-highlight-color), [CSS-Tricks tap highlight](https://css-tricks.com/snippets/css/remove-gray-highlight-when-tapping-links-in-mobile-safari/)

---

### 4.5 Overscroll Behavior

**Verified with corrections from adversarial verification pass:**

**`overscroll-behavior` on iOS — nuanced support:**
- `overscroll-behavior` is supported in Safari since iOS 16 (September 2022). The "Widely Available" baseline was reached 2025-03-12, then **reverted to Limited Availability ~April 2026** due to remaining cross-browser inconsistencies.
- **Critical Safari quirk:** Safari honors the property on `<html>`, not `<body>` alone. Most "it doesn't work" reports are caused by applying it only to `body`. Apply to both:

```css
/* Apply to BOTH — Safari honors html, Chrome/Android honors body */
html, body {
  overscroll-behavior-y: none; /* removes rubber-band bounce */
  /* OR */
  overscroll-behavior-y: contain; /* keeps bounce, prevents chaining */
}

/* Prevent overscroll chaining on inner scroll containers */
.scroll-container {
  overscroll-behavior: contain;
}
```

**Known remaining bugs on iOS (as of July 2026):**
- Broken in scroll-snap containers (WebKit Bug 240235)
- Horizontal swipe-back navigation cannot be blocked (WebKit Bug 240183 — apparently intentional)
- May fail in PWA + `viewport-fit=cover` combination (WebKit Bug 237961) — test on device

**Pull-to-refresh in iOS standalone PWA — IMPORTANT CORRECTION:**
- **iOS standalone PWAs do NOT have native pull-to-refresh.** This gesture belongs to Safari's browser chrome, which is absent in standalone mode. There is nothing to disable.
- Developers actually file feature requests to *add* pull-to-refresh to iOS standalone PWAs; it is absent by default.
- **Chrome on Android standalone PWAs DO have pull-to-refresh** enabled by default. Disable with `overscroll-behavior-y: contain` on `body`.

**Rubber-band bounce (iOS):** The native iOS elastic bounce effect persists in standalone mode. `overscroll-behavior-y: none` on `html` suppresses it on iOS 16+. Edge-swipe gestures (back navigation) cannot be disabled via CSS — only via `touchstart` + `preventDefault` JS interception at the edge zones.

**Scroll lock pattern (robust fallback):**
```css
/* Avoid position: fixed directly on body (iOS 16.6+ cropping bug) */
/* Use: */
html { overflow: hidden; }
body { overflow: hidden; }
.app-root { height: 100dvh; overflow-y: auto; overscroll-behavior-y: contain; }
```

Sources: [web.dev app-design](https://web.dev/learn/pwa/app-design), [magicbell.com PWA guide](https://www.magicbell.com/blog/pwa-ios-limitations-safari-support-complete-guide), [matuzo.at overscroll-behavior](https://www.matuzo.at/blog/2022/100daysof-day53), [WebKit Bug 237961](https://bugs.webkit.org/show_bug.cgi?id=237961), [WebKit Bug 240235](https://bugs.webkit.org/show_bug.cgi?id=240235)

---

### 4.6 Other iOS PWA Constraints (Verified)

| Feature | Status |
|---|---|
| Push Notifications | Available since iOS 16.4 (requires Home Screen installation first) |
| Background Sync | Not reliably supported |
| Web Bluetooth / NFC | Not available |
| `beforeinstallprompt` | Not available on iOS |
| Fullscreen API | Not available |
| Service Workers | Supported (improved stability in iOS 17+) |
| Storage quota | Conservative (~50MB practical limit); cache can be evicted |
| Badge API | Available since iOS 16.4 |
| `display-mode: standalone` | Supported |

**Cache strategy:** Re-cache critical assets on every launch; handle cache misses gracefully; keep cache under 50MB.

Sources: [brainhub.eu PWA iOS](https://brainhub.eu/library/pwa-on-ios), [magicbell.com PWA limitations](https://www.magicbell.com/blog/pwa-ios-limitations-safari-support-complete-guide), [bswen.com Safari PWA limitations](https://docs.bswen.com/blog/2026-03-12-safari-pwa-limitations-ios/)

---

## Summary: Key Design Decisions for This Project

| Decision | Recommendation | Confidence |
|---|---|---|
| Color approach | Pastel-base (mint/soft teal) + vibrant accent (coral CTA) | High |
| Corner radius | 12–16px cards, pill buttons | High |
| Primary typeface | Figtree (headings) + Nunito (body) | Medium-High |
| Dark mode | Required; desaturated tinted-dark; luminance-based elevation | High |
| Touch targets | 48px minimum (MD3 cross-platform safe) | High |
| Navigation | Bottom tab bar (2–3 tabs: Lists, Profile/Settings) | High |
| FAB | Bottom-right for primary "Add Item" action | High |
| Swipe gestures | Left-swipe to reveal delete; always provide undo | High |
| Haptic feedback | Progressive enhancement only; no iOS support without brittle workaround | High |
| Celebration pattern | Animated checkmark + confetti burst on list completion; sparse | Medium |
| Animation library | `motion` (v12) for transitions; `canvas-confetti` for celebrations; `@lottiefiles/dotlottie-react` for Lottie | High |
| Viewport height | `min-height: 100svh; height: 100dvh` | High |
| Safe areas | `env(safe-area-inset-*)` with `viewport-fit=cover`; scope to `(display-mode: standalone)` | High |
| Overscroll | `overscroll-behavior-y: contain` on body | High |
| Tap highlight | Remove with `transparent`; add `:active` state | High |

---

## Sources

### Primary / Official
- [Apple HIG Design Tips](https://developer.apple.com/design/tips)
- [Material Design 3 — Touch Target](https://m2.material.io/develop/web/supporting/touch-target)
- [Material Design 3 — FAB Guidelines](https://m3.material.io/components/floating-action-button/guidelines)
- [Material Design 3 — Shape / Corner Radius](https://m3.material.io/styles/shape/corner-radius-scale)
- [MDN — -webkit-tap-highlight-color](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/-webkit-tap-highlight-color)
- [web.dev — Viewport Units](https://web.dev/blog/viewport-units)
- [web.dev — PWA App Design](https://web.dev/learn/pwa/app-design)
- [Can I Use — Viewport Unit Variants](https://caniuse.com/viewport-unit-variants)
- [W3C — Vibration API Obsolete Proposal](https://lists.w3.org/Archives/Public/public-review-announce/2024Sep/0003.html)

### Libraries
- [motion.dev](https://motion.dev/)
- [Motion Upgrade Guide](https://motion.dev/docs/react-upgrade-guide)
- [canvas-confetti npm](https://www.npmjs.com/package/canvas-confetti)
- [react-confetti npm](https://www.npmjs.com/package/react-confetti)
- [@lottiefiles/dotlottie-react npm](https://www.npmjs.com/package/@lottiefiles/dotlottie-react)
- [react-spring.dev](https://www.react-spring.dev/)
- [MagicUI Confetti component](https://magicui.design/docs/components/confetti)
- [ios-vibrator-pro-max GitHub](https://github.com/samdenty/ios-vibrator-pro-max)
- [vibrator.dev](https://vibrator.dev/)

### Design Research & Case Studies
- [Tubik Studio — Mascots in UI](https://tubikstudio.com/blog/design-me-live-the-power-of-mascots-in-ui-and-branding/)
- [Duolingo Design Breakdown](https://www.925studios.co/blog/duolingo-design-breakdown)
- [Toshl Finance Dashboard](https://www.fusioncharts.com/blog/personal-finance-apps-with-amazing-dashboards-part-2-toshl-finance/)
- [NNGroup — Bottom Sheet](https://www.nngroup.com/articles/bottom-sheet/)
- [NNGroup — Mobile Navigation Patterns](https://www.nngroup.com/articles/mobile-navigation-patterns/)
- [NNGroup — Contextual Swipe](https://www.nngroup.com/articles/contextual-swipe/)
- [Material 3 Expressive (Supercharge.design)](https://supercharge.design/blog/material-3-expressive)

### PWA / iOS References
- [MagicBell — PWA iOS Limitations 2026](https://www.magicbell.com/blog/pwa-ios-limitations-safari-support-complete-guide)
- [Brainhub — PWA on iOS 2025](https://brainhub.eu/library/pwa-on-ios)
- [Vinova — Safari iOS PWA Tips](https://vinova.sg/navigating-safari-ios-pwa-limitations/)
- [iPadOS 26 PWA Report (dev.to)](https://dev.to/reinhart1010/pwa-in-ipados-26-is-a-joke-38g1)
- [firt.dev PWA Design Tips](https://firt.dev/pwa-design-tips/)
- [Progressier — Vibration API](https://progressier.com/pwa-capabilities/vibration-api)
- [LambdaTest — Vibration API Browser Support](https://www.testmuai.com/learning-hub/vibration-api-browser-support/)

### Navigation & Interaction
- [Acclaim — Hamburger vs Tab Bar](https://acclaim.agency/blog/the-future-of-mobile-navigation-hamburger-menus-vs-tab-bars)
- [Diverse Website Design — Thumb Zones 2025](https://diversewebsitedesign.com.au/designing-for-thumb-zones-mobile-ux-in-2025/)
- [Tim Graf — Thumb Zone Guide](https://timgraf.com/ux-design/designing-for-the-thumb-zone-a-modern-guide-to-mobile-ux-that-respects-human-anatomy/)
- [LogRocket — Bottom Sheets](https://blog.logrocket.com/ux-design/bottom-sheets-optimized-ux/)
- [LogRocket — Swipe UX Accessibility](https://blog.logrocket.com/ux-design/accessible-swipe-contextual-action-triggers/)
- [CSS-Tricks — Tap Highlight](https://css-tricks.com/snippets/css/remove-gray-highlight-when-tapping-links-in-mobile-safari/)
