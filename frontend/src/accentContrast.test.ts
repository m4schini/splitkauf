/// <reference types="node" />
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

// WCAG 2.2 AA requires >=4.5:1 contrast for normal text and >=3:1 for UI
// components / graphical objects (focus rings, checkboxes). The icon-derived
// accent green (~#7abf7e, ~2.5:1 on white) fails the text threshold outright,
// so the tokens in index.css are contrast-derived shades, not the raw icon
// color, in the light theme (the raw icon green is however AA-safe for the
// dark theme, since it sits on a much darker background there).
//
// This test parses the actual accent-related custom properties out of
// index.css (rather than hardcoding hex values here) so a future edit to the
// CSS that regresses a pairing fails CI instead of requiring a manual audit.

const dirname = path.dirname(fileURLToPath(import.meta.url))
const cssPath = path.resolve(dirname, 'index.css')
const css = readFileSync(cssPath, 'utf-8')

function extractBlock(css: string, selectorRegex: RegExp): string {
  const match = selectorRegex.exec(css)
  if (!match) throw new Error(`could not find block for ${selectorRegex}`)
  const start = match.index + match[0].length
  let depth = 1
  let i = start
  while (depth > 0 && i < css.length) {
    if (css[i] === '{') depth++
    else if (css[i] === '}') depth--
    i++
  }
  return css.slice(start, i - 1)
}

function extractVar(block: string, name: string): string {
  const re = new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})\\s*;`)
  const match = re.exec(block)
  if (!match) throw new Error(`could not find --${name} in block`)
  return match[1]
}

// :root { ... } — the first top-level rule, i.e. the light theme.
const rootBlock = extractBlock(css, /:root\s*\{/)
// @media (prefers-color-scheme: dark) { :root { ... } } — the dark theme.
const darkMediaBlock = extractBlock(css, /@media \(prefers-color-scheme: dark\)\s*\{/)
const darkRootBlock = extractBlock(darkMediaBlock, /:root\s*\{/)

interface Theme {
  name: string
  accent: string
  accentContrast: string
  focusRing: string
  bg: string
  bgElevated: string
  bgElevatedHover: string
  fgMuted: string
}

const light: Theme = {
  name: 'light',
  accent: extractVar(rootBlock, 'accent'),
  accentContrast: extractVar(rootBlock, 'accent-contrast'),
  focusRing: extractVar(rootBlock, 'focus-ring'),
  bg: extractVar(rootBlock, 'bg'),
  bgElevated: extractVar(rootBlock, 'bg-elevated'),
  bgElevatedHover: extractVar(rootBlock, 'bg-elevated-hover'),
  fgMuted: extractVar(rootBlock, 'fg-muted'),
}

const dark: Theme = {
  name: 'dark',
  accent: extractVar(darkRootBlock, 'accent'),
  accentContrast: extractVar(darkRootBlock, 'accent-contrast'),
  focusRing: extractVar(darkRootBlock, 'focus-ring'),
  bg: extractVar(darkRootBlock, 'bg'),
  bgElevated: extractVar(darkRootBlock, 'bg-elevated'),
  bgElevatedHover: extractVar(darkRootBlock, 'bg-elevated-hover'),
  fgMuted: extractVar(darkRootBlock, 'fg-muted'),
}

function srgbToLinear(c: number): number {
  const cs = c / 255
  return cs <= 0.03928 ? cs / 12.92 : Math.pow((cs + 0.055) / 1.055, 2.4)
}

function relativeLuminance(hex: string): number {
  const h = hex.replace('#', '')
  const r = parseInt(h.slice(0, 2), 16)
  const g = parseInt(h.slice(2, 4), 16)
  const b = parseInt(h.slice(4, 6), 16)
  return 0.2126 * srgbToLinear(r) + 0.7152 * srgbToLinear(g) + 0.0722 * srgbToLinear(b)
}

function contrastRatio(hexA: string, hexB: string): number {
  const lA = relativeLuminance(hexA)
  const lB = relativeLuminance(hexB)
  const lighter = Math.max(lA, lB)
  const darker = Math.min(lA, lB)
  return (lighter + 0.05) / (darker + 0.05)
}

const TEXT_AA = 4.5
const UI_AA = 3.0

describe.each([light, dark])('$name theme accent contrast (WCAG 2.2 AA)', (theme) => {
  it('accent-contrast text on accent background (button text, .unsynced-tag) >= 4.5:1', () => {
    expect(contrastRatio(theme.accentContrast, theme.accent)).toBeGreaterThanOrEqual(TEXT_AA)
  })

  it('accent focus ring vs page background (:focus-visible outline) >= 3:1', () => {
    expect(contrastRatio(theme.focusRing, theme.bg)).toBeGreaterThanOrEqual(UI_AA)
  })

  it('accent checkbox vs row background (.item-checkbox on .row) >= 3:1', () => {
    expect(contrastRatio(theme.accent, theme.bgElevated)).toBeGreaterThanOrEqual(UI_AA)
  })

  it('accent vs elevated-hover background >= 3:1', () => {
    expect(contrastRatio(theme.accent, theme.bgElevatedHover)).toBeGreaterThanOrEqual(UI_AA)
  })

  it('accent vs base background >= 3:1 (UI component floor)', () => {
    expect(contrastRatio(theme.accent, theme.bg)).toBeGreaterThanOrEqual(UI_AA)
  })

  // Pre-existing checked/muted-text tokens (--fg-muted, used for checked item
  // names/quantities and secondary text) are unrelated to the accent tokens
  // but this phase's acceptance criteria call for re-confirming they still
  // clear AA text contrast after the accent swap.
  it('muted text vs base background (checked items, hints) >= 4.5:1', () => {
    expect(contrastRatio(theme.fgMuted, theme.bg)).toBeGreaterThanOrEqual(TEXT_AA)
  })

  it('muted text vs elevated background (row counts, notes) >= 4.5:1', () => {
    expect(contrastRatio(theme.fgMuted, theme.bgElevated)).toBeGreaterThanOrEqual(TEXT_AA)
  })
})
