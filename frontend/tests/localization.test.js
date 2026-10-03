import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { get } from 'svelte/store'
import {
  languages,
  translator,
  translate,
  normalizeLocale,
  loadLocale,
  locale
} from '../src/stores/i18n.js'

test('all six languages have the same messages and interpolation variables', () => {
  const read = (lang) =>
    JSON.parse(
      readFileSync(new URL(`../public/locales/${lang}.json`, import.meta.url))
    )
  const english = read('en')
  const placeholders = (text) =>
    [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort()
  for (const language of languages) {
    const messages = read(language.id)
    assert.deepEqual(Object.keys(messages).sort(), Object.keys(english).sort())
    for (const key of Object.keys(english)) {
      assert.ok(messages[key].trim(), `${language.id}: ${key}`)
      assert.deepEqual(
        placeholders(messages[key]),
        placeholders(english[key]),
        `${language.id}: ${key}`
      )
    }
  }
})
test('locale switches immediately notify UI subscribers and normalize regional tags', async () => {
  const seen = []
  const unsubscribe = translator.subscribe((t) => seen.push(t('New folder')))
  await loadLocale('de-DE')
  await loadLocale('fr-CA')
  await loadLocale('es')
  await loadLocale('it')
  await loadLocale('pt-BR')
  assert.deepEqual(seen.slice(-5), [
    'Neuer Ordner',
    'Nouveau dossier',
    'Nueva carpeta',
    'Nuova cartella',
    'Nova pasta'
  ])
  unsubscribe()
  assert.equal(normalizeLocale('unknown'), 'en')
  assert.equal(
    translate('de', 'Created “{name}”', { name: '$& {path}' }),
    '„$& {path}“ erstellt'
  )
  assert.equal(translate('es', 'Future message'), 'Future message')
  locale.set('en')
})
