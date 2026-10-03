import { test } from 'node:test'
import assert from 'node:assert/strict'
const storage = new Map([['access_token', 'test-access']])
globalThis.localStorage = {
  getItem: (key) => storage.get(key) || null,
  setItem: (key, value) => storage.set(key, value),
  removeItem: (key) => storage.delete(key)
}
globalThis.document = { cookie: 'csrf_token=test-csrf' }
const { destinationPath, topLevelPaths, transferFiles } =
  await import('../src/lib/operations.js')
const { locale } = await import('../src/stores/i18n.js')
locale.set('en')

test('nested selections transfer once, and destination paths stay in the workspace', () => {
  assert.deepEqual(topLevelPaths(['docs/a.txt', 'docs', 'photos', 'docs']), [
    'docs',
    'photos'
  ])
  assert.equal(destinationPath('docs/a.txt', '/archive/'), 'archive/a.txt')
  assert.throws(() => destinationPath('a', '../outside'))
  assert.throws(() => destinationPath('a', 'archive', '../bad'))
})
test('a partial batch failure preserves the failed item and reports successful ones', async () => {
  const calls = []
  globalThis.fetch = async (url, options) => {
    const body = JSON.parse(options.body)
    calls.push(body)
    return body.src === 'a.txt'
      ? new Response(null, { status: 201 })
      : new Response('file already exists', { status: 400 })
  }
  const result = await transferFiles(['a.txt', 'b.txt'], 'destination', 'copy')
  assert.deepEqual(calls, [
    { src: 'a.txt', dst: 'destination/a.txt' },
    { src: 'b.txt', dst: 'destination/b.txt' }
  ])
  assert.equal(result[0].ok, true)
  assert.equal(result[1].ok, false)
  assert.equal(result[1].path, 'b.txt')
  assert.match(result[1].error, /already exists/i)
})
test('copying into the source and moving onto itself never send requests', async () => {
  globalThis.fetch = async () => {
    throw new Error('unexpected network request')
  }
  assert.equal(
    (await transferFiles(['docs'], 'docs/child', 'copy'))[0].ok,
    false
  )
  assert.equal((await transferFiles(['docs'], '', 'move'))[0].ok, false)
})
