import { writable } from 'svelte/store'
let saved = {}
try {
  saved = JSON.parse(localStorage.getItem('fileship_preferences') || '{}')
} catch {}
export const preferences = writable({
  hideDotfiles: false,
  singleClick: true,
  exactDates: false,
  ...saved
})
preferences.subscribe((value) =>
  localStorage.setItem('fileship_preferences', JSON.stringify(value))
)
