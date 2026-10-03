import { writable, derived, get } from 'svelte/store'
import en from '../../public/locales/en.json' with { type: 'json' }
import de from '../../public/locales/de.json' with { type: 'json' }
import fr from '../../public/locales/fr.json' with { type: 'json' }
import es from '../../public/locales/es.json' with { type: 'json' }
import it from '../../public/locales/it.json' with { type: 'json' }
import pt from '../../public/locales/pt.json' with { type: 'json' }

export const languages = [
  { id: 'en', name: 'English' },
  { id: 'de', name: 'Deutsch' },
  { id: 'fr', name: 'Français' },
  { id: 'es', name: 'Español' },
  { id: 'it', name: 'Italiano' },
  { id: 'pt', name: 'Português' }
]
const dictionaries = { en, de, fr, es, it, pt }
export function normalizeLocale(value) {
  const language = String(value || '')
    .toLowerCase()
    .split(/[-_]/)[0]
  return Object.hasOwn(dictionaries, language) ? language : 'en'
}
let saved
try {
  saved = localStorage.getItem('locale')
} catch {}
export const locale = writable(
  normalizeLocale(saved || globalThis.navigator?.language)
)
export const messages = derived(
  locale,
  (lang) => dictionaries[normalizeLocale(lang)]
)
export function translate(lang, key, variables = {}) {
  const dictionary = dictionaries[normalizeLocale(lang)]
  const text = dictionary[key] ?? en[key] ?? key
  return text.replace(/\{(\w+)\}/g, (match, name) =>
    Object.hasOwn(variables, name) ? String(variables[name]) : match
  )
}
export const translator = derived(
  locale,
  (lang) => (key, variables) => translate(lang, key, variables)
)
export function t(key, variables) {
  return get(translator)(key, variables)
}
export async function loadLocale(lang) {
  locale.set(normalizeLocale(lang))
}
locale.subscribe((value) => {
  const lang = normalizeLocale(value)
  try {
    localStorage.setItem('locale', lang)
  } catch {}
  if (typeof document !== 'undefined' && document.documentElement)
    document.documentElement.lang = lang
})

export function translateError(message) {
  const raw = String(message || '')
  const rules = [
    [/file exists|already exists/i, 'File already exists'],
    [/permission denied|forbidden/i, 'Permission denied'],
    [/storage quota exceeded/i, 'Storage quota exceeded'],
    [/invalid credentials/i, 'Invalid credentials'],
    [/file type not allowed/i, 'File type not allowed']
  ]
  return t(
    rules.find(([pattern]) => pattern.test(raw))?.[1] || raw || 'Request failed'
  )
}
