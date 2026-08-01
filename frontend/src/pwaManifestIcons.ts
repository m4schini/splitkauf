// Single source of truth for the PWA manifest's icon set, shared between
// vite.config.ts (VitePWA manifest.icons) and iconsBuild.test.ts, so the
// test can assert on the configured icon list without running a build.
export const pwaManifestIcons = [
  {
    src: '/icons/icon-192.png',
    sizes: '192x192',
    type: 'image/png',
  },
  {
    src: '/icons/icon-512.png',
    sizes: '512x512',
    type: 'image/png',
  },
  {
    src: '/icons/icon-512-maskable.png',
    sizes: '512x512',
    type: 'image/png',
    purpose: 'maskable',
  },
]
