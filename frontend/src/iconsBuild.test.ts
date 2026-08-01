/// <reference types="node" />
import { statSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { pwaManifestIcons } from './pwaManifestIcons'

// Verifies the PWA icon set used by the app (see hack/generate-icons.mjs for
// how the source PNGs under frontend/public/icons/ are generated from
// frontend/src/assets/appicon.png).
//
// This checks the committed source artifacts under frontend/public/ and the
// manifest icon list configured in vite.config.ts (via pwaManifestIcons.ts)
// directly, without running `vite build`. Running a production build here
// would be slow/flaky under vitest's timeout and — critically — vite.config.ts
// sets build.outDir to '../ports/web/dist' with emptyOutDir: true, so a
// `vite build` invoked from a unit test would wipe and rebuild the real
// dist directory that the Go ports/web layer embeds/serves.

const dirname = path.dirname(fileURLToPath(import.meta.url))
const frontendRoot = path.resolve(dirname, '..')
const publicDir = path.resolve(frontendRoot, 'public')

// Placeholder solid-color icons were a few hundred bytes to ~2KB; the real
// artwork rendered from appicon.png is tens of KB at minimum, so this
// threshold safely distinguishes "real icon" from "placeholder".
const MIN_REAL_ICON_BYTES = 5_000

describe('PWA manifest icon config and icon artifacts', () => {
  it('manifest icons list the three configured icon entries', () => {
    expect(pwaManifestIcons).toEqual([
      { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' },
      { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png' },
      {
        src: '/icons/icon-512-maskable.png',
        sizes: '512x512',
        type: 'image/png',
        purpose: 'maskable',
      },
    ])
  })

  it.each(['icon-192.png', 'icon-512.png', 'icon-512-maskable.png', 'icon-180.png'])(
    '%s is real artwork, not a solid-color placeholder',
    (file) => {
      const { size } = statSync(path.join(publicDir, 'icons', file))
      expect(size).toBeGreaterThan(MIN_REAL_ICON_BYTES)
    },
  )

  it('favicon.png is real artwork, not a solid-color placeholder', () => {
    const { size } = statSync(path.join(publicDir, 'favicon.png'))
    expect(size).toBeGreaterThan(MIN_REAL_ICON_BYTES / 2)
  })
})
