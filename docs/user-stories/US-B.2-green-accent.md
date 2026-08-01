# US-B.2 — Green accent

**Milestone:** M6
**Depends on:** US-B.1

**As a** member, **I want** the app's accent color to match the green of the
app icon, **so that** the installed app and its UI feel like one product.

## Acceptance criteria

- The UI accent (primary buttons, checkboxes, focus rings, links, checked
  states) is a green derived from the app-icon green (≈ `#7abf7e`), replacing
  the current accent in both themes.
- The exact shades are chosen for contrast, not sampled literally: accent
  text/controls meet WCAG 2.2 AA (4.5:1 text, 3:1 UI) against their
  backgrounds in light *and* dark mode (the raw icon green fails AA on white
  — a darker green is required for the light theme).
- The manifest `theme_color` and the iOS status-bar treatment follow the new
  accent (replacing `#007aff`).
- The UX guardrails checklist (research §6) still passes; no meaning is
  conveyed by color alone.
