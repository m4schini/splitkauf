# US-B.1 — App icon

**Milestone:** M6
**Depends on:** US-O.1

**As a** member, **I want** Splitkauf's real app icon on my home screen and in
my browser tab, **so that** the installed app is recognizable instead of a
placeholder square.

## Acceptance criteria

- All PWA icons are derived from `frontend/src/assets/appicon.png` (green
  rounded square, white checklist sheet, green check): the manifest icons
  (192, 512, 512-maskable) and the `apple-touch-icon` (180), replacing the
  placeholder solid-color PNGs in `frontend/public/icons/`.
- The maskable variant keeps the checklist artwork inside the maskable safe
  zone (nothing clipped under Android's mask shapes).
- The favicon is derived from the same artwork (replacing
  `frontend/public/favicon.svg` or regenerating it to match).
- An install on iOS Safari and Android shows the real icon on the home
  screen; the browser tab shows the matching favicon.
