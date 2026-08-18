import { writable, get } from 'svelte/store'

export const locale = writable(localStorage.getItem('locale') || navigator.language?.slice(0, 2) || 'en')
export const messages = writable({})

const cache = {}

export async function loadLocale(lang) {
  if (cache[lang]) {
    messages.set(cache[lang])
    return
  }
  try {
    const res = await fetch(`/locales/${lang}.json`)
    if (!res.ok) throw new Error()
    const data = await res.json()
    cache[lang] = data
    messages.set(data)
  } catch {
    if (lang !== 'en') await loadLocale('en')
  }
}

locale.subscribe(lang => {
  localStorage.setItem('locale', lang)
  loadLocale(lang)
})

// t('key', { name: 'foo' }) → ersetzt {name} mit foo
export function t(key, vars = {}) {
  const msgs = get(messages)
  let str = msgs[key] || key
  for (const [k, v] of Object.entries(vars)) {
    str = str.replaceAll(`{${k}}`, v)
  }
  return str
}
