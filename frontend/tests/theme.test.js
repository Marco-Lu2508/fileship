import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { JSDOM } from 'jsdom'

const dom = new JSDOM('<html><head><meta name="theme-color"></head></html>', {
  url: 'http://localhost'
})
globalThis.document = dom.window.document
globalThis.localStorage = dom.window.localStorage
globalThis.getComputedStyle = dom.window.getComputedStyle
localStorage.setItem('theme', 'removed-design')
const { theme, THEMES, normalizeTheme } = await import('../src/stores/theme.js')
const css = readFileSync(
  new URL('../src/styles/themes.css', import.meta.url),
  'utf8'
)
const palettes = Object.fromEntries(
  [...css.matchAll(/html\[data-theme='([^']+)'\]\s*\{([^}]+)\}/g)].map(
    ([, id, rules]) => [
      id,
      Object.fromEntries(
        [...rules.matchAll(/--([\w-]+):\s*(#[\da-fA-F]{6});/g)].map(
          ([, key, color]) => [key, color]
        )
      )
    ]
  )
)
const luminance = (hex) => {
  const rgb = [1, 3, 5]
    .map((offset) => parseInt(hex.slice(offset, offset + 2), 16) / 255)
    .map((value) =>
      value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
    )
  return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722
}
const contrast = (a, b) => {
  const pair = [luminance(a), luminance(b)].sort((a, b) => b - a)
  return (pair[0] + 0.05) / (pair[1] + 0.05)
}

test('stored designs recover safely and all choices update the page and persist', () => {
  assert.equal(document.documentElement.dataset.theme, 'light')
  assert.equal(normalizeTheme('unknown'), 'light')
  for (const design of THEMES) {
    document.documentElement.style.setProperty(
      '--sidebar-bg',
      palettes[design.id]['sidebar-bg']
    )
    theme.set(design.id)
    assert.equal(document.documentElement.dataset.theme, design.id)
    assert.equal(localStorage.getItem('theme'), design.id)
    assert.equal(
      document.querySelector('meta').content,
      palettes[design.id]['sidebar-bg']
    )
  }
})

test('every design has readable file metadata, navigation and primary actions', () => {
  const navigationColors = new Set()
  for (const design of THEMES) {
    const p = palettes[design.id]
    assert.ok(p, `Missing palette: ${design.id}`)
    navigationColors.add(p['sidebar-bg'])
    for (const [foreground, background] of [
      ['text', 'surface'],
      ['text2', 'surface'],
      ['text3', 'surface2'],
      ['sidebar-text', 'sidebar-bg'],
      ['sidebar-muted', 'sidebar-bg'],
      ['sidebar-on-active', 'sidebar-active'],
      ['on-accent', 'accent'],
      ['on-accent', 'accent-h']
    ])
      assert.ok(
        contrast(p[foreground], p[background]) >= 4.5,
        `${design.id}: ${foreground}/${background}`
      )
  }
  assert.equal(navigationColors.size, THEMES.length)
})
