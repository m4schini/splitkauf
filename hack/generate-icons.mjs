#!/usr/bin/env node
// Renders the PWA/browser icon set from the 1024px source artwork.
//
// Run with:
//   node hack/generate-icons.mjs
//
// Re-run this script whenever frontend/src/assets/appicon.png (the source
// artwork) changes; the generated PNGs under frontend/public/icons/ and
// frontend/public/favicon.png are committed as build artifacts, not derived
// at build time.
//
// Outputs (all read from frontend/src/assets/appicon.png):
//   frontend/public/icons/icon-192.png           192x192 direct resize
//   frontend/public/icons/icon-512.png           512x512 direct resize
//   frontend/public/icons/icon-180.png           180x180 direct resize (apple-touch-icon)
//   frontend/public/icons/icon-512-maskable.png  512x512, artwork scaled to
//                                                 ~80% and centered on a
//                                                 solid icon-green (#7abf7e)
//                                                 background so Android/iOS
//                                                 mask shapes never clip it
//   frontend/public/favicon.png                  48x48 direct resize

import { mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(__dirname, '..')

// sharp is a devDependency of frontend/package.json, not the repo root, so
// resolve it relative to frontend/ explicitly rather than via a bare import
// (which would only see node_modules ancestors of this file's own path).
const require = createRequire(path.join(repoRoot, 'frontend/package.json'))
const sharp = require('sharp')

const SOURCE = path.join(repoRoot, 'frontend/src/assets/appicon.png')
const ICONS_DIR = path.join(repoRoot, 'frontend/public/icons')
const PUBLIC_DIR = path.join(repoRoot, 'frontend/public')

const ICON_GREEN = '#7abf7e'

async function directResize(size, outFile) {
  await sharp(SOURCE).resize(size, size, { fit: 'cover' }).png().toFile(outFile)
  console.log(`wrote ${path.relative(repoRoot, outFile)} (${size}x${size})`)
}

async function maskableIcon(size, outFile) {
  const artworkSize = Math.round(size * 0.8)
  const artwork = await sharp(SOURCE)
    .resize(artworkSize, artworkSize, { fit: 'cover' })
    .png()
    .toBuffer()

  const offset = Math.round((size - artworkSize) / 2)

  await sharp({
    create: {
      width: size,
      height: size,
      channels: 4,
      background: ICON_GREEN,
    },
  })
    .composite([{ input: artwork, left: offset, top: offset }])
    .png()
    .toFile(outFile)
  console.log(
    `wrote ${path.relative(repoRoot, outFile)} (${size}x${size}, ${Math.round(size * 0.8)}px artwork on ${ICON_GREEN})`,
  )
}

async function main() {
  await mkdir(ICONS_DIR, { recursive: true })

  await directResize(192, path.join(ICONS_DIR, 'icon-192.png'))
  await directResize(512, path.join(ICONS_DIR, 'icon-512.png'))
  await directResize(180, path.join(ICONS_DIR, 'icon-180.png'))
  await maskableIcon(512, path.join(ICONS_DIR, 'icon-512-maskable.png'))
  await directResize(48, path.join(PUBLIC_DIR, 'favicon.png'))
}

main().catch((err) => {
  console.error(err)
  process.exitCode = 1
})
