import { writable } from 'svelte/store'

export const THEMES = [
  { id: 'dark',    label: 'Dark' },
  { id: 'light',   label: 'Light' },
  { id: 'nord',    label: 'Nord' },
  { id: 'solarized', label: 'Solarized' },
  { id: 'gruvbox', label: 'Gruvbox' },
]

const stored = localStorage.getItem('theme') || 'dark'
export const theme = writable(stored)

theme.subscribe(t => {
  localStorage.setItem('theme', t)
  document.documentElement.setAttribute('data-theme', t)
})
