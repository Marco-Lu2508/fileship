import { writable } from 'svelte/store'

export const THEMES = [
  { id: 'dark', label: 'Dark' },
  { id: 'light', label: 'Light' },
  { id: 'ocean', label: 'Ocean' },
  { id: 'violet', label: 'Violet' },
  { id: 'nord', label: 'Nord' },
  { id: 'solarized', label: 'Solarized' },
  { id: 'gruvbox', label: 'Gruvbox' }
]

export function normalizeTheme(value) {
  return THEMES.some((item) => item.id === value) ? value : 'light'
}

const stored = normalizeTheme(localStorage.getItem('theme'))
export const theme = writable(stored)

export function applyTheme(t) {
  t = normalizeTheme(t)
  localStorage.setItem('theme', t)
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-theme', t)
    const color = getComputedStyle(document.documentElement)
      .getPropertyValue('--sidebar-bg')
      .trim()
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute('content', color)
  }
}

theme.subscribe(applyTheme)
