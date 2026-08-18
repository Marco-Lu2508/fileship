import { writable } from 'svelte/store'

const stored = localStorage.getItem('theme') || 'dark'
export const theme = writable(stored)

theme.subscribe(t => {
  localStorage.setItem('theme', t)
  document.documentElement.setAttribute('data-theme', t)
})
