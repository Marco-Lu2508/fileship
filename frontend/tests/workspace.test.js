import { test, beforeEach, afterEach } from 'node:test'
import assert from 'node:assert/strict'
import { get } from 'svelte/store'
import { formatBytes, fileKind, validName } from '../src/lib/files.js'

const storage = new Map()
globalThis.localStorage = {
  getItem: (key) => storage.get(key) ?? null,
  setItem: (key, value) => storage.set(key, String(value)),
  removeItem: (key) => storage.delete(key)
}
globalThis.document = { cookie: 'csrf_token=test-csrf' }
const auth = await import('../src/stores/auth.js')
const workspace = await import('../src/stores/files.js')
const json = (value) =>
  new Response(JSON.stringify(value), {
    headers: { 'Content-Type': 'application/json' }
  })

beforeEach(() => {
  storage.clear()
  localStorage.setItem('access_token', 'old-access')
  localStorage.setItem('refresh_token', 'old-refresh')
  auth.accessToken.set('old-access')
  auth.user.set(null)
})
afterEach(async () => {
  globalThis.fetch = async () => new Response(null, { status: 204 })
  await auth.logout()
})

test('formatting handles zero, gigabytes, terabytes and invalid values', () => {
  assert.equal(formatBytes(0), '0 B')
  assert.equal(formatBytes(1024 ** 3), '1 GB')
  assert.equal(formatBytes(1024 ** 4), '1 TB')
  assert.equal(formatBytes(NaN), '0 B')
  assert.equal(
    fileKind({ name: 'report.PDF', mime_type: 'application/pdf' }),
    'text'
  )
  for (const name of ['', '  ', '..', 'a/b', 'a\\b', 'bad\nname'])
    assert.equal(validName(name), false)
})
test('failed mutations propagate server errors instead of reporting success', async () => {
  globalThis.fetch = async () =>
    new Response('permission denied', { status: 403 })
  await assert.rejects(workspace.mkdir('restricted'), /permission denied/)
  await assert.rejects(workspace.deleteFile('restricted'), /permission denied/)
  await assert.rejects(workspace.renameFile('a', 'b'), /permission denied/)
})
test('folder loading discards a late response after navigation', async () => {
  let release
  globalThis.fetch = async (url) =>
    String(url).includes('path=first')
      ? new Promise((resolve) => (release = resolve))
      : json({
          files: [{ name: 'new.txt', path: 'second/new.txt' }],
          total: 1,
          page: 1,
          per_page: 100
        })
  const first = workspace.loadFiles('first')
  await workspace.loadFiles('second')
  release(
    json({ files: [{ name: 'old.txt' }], total: 1, page: 1, per_page: 100 })
  )
  await first
  assert.equal(get(workspace.currentPath), 'second')
  assert.equal(get(workspace.files).files[0].name, 'new.txt')
  assert.equal(get(workspace.loading), false)
})
test('list failure exposes a retryable error and clears stale files', async () => {
  globalThis.fetch = async () =>
    new Response('folder unavailable', { status: 500 })
  await workspace.loadFiles('broken')
  assert.equal(get(workspace.loadError), 'folder unavailable')
  assert.deepEqual(get(workspace.files).files, [])
})
test('page and server sorting are preserved across live refresh', async () => {
  const urls = []
  globalThis.fetch = async (url) => {
    urls.push(new URL(url, 'http://localhost'))
    return json({ files: [], total: 300, page: 2, per_page: 100 })
  }
  await workspace.loadFiles('many', {
    page: 2,
    sort_by: 'size',
    sort_asc: false
  })
  await workspace.loadFiles('many')
  assert.equal(urls[1].searchParams.get('page'), '2')
  assert.equal(urls[1].searchParams.get('sort_by'), 'size')
  assert.equal(urls[1].searchParams.get('sort_asc'), 'false')
})
test('concurrent expired requests share one CSRF-protected refresh', async () => {
  let refreshes = 0
  globalThis.fetch = async (url, options) => {
    if (url === '/api/auth/refresh') {
      refreshes++
      assert.equal(options.headers['X-CSRF-Token'], 'test-csrf')
      await new Promise((resolve) => setTimeout(resolve, 10))
      return json({ access_token: 'new-access', refresh_token: 'new-refresh' })
    }
    return options.headers.Authorization === 'Bearer new-access'
      ? json({ ok: true })
      : new Response(null, { status: 401 })
  }
  const results = await Promise.all([
    auth.apiFetch('/api/me'),
    auth.apiFetch('/api/files'),
    auth.apiFetch('/api/me/favorites')
  ])
  assert.equal(refreshes, 1)
  assert.ok(results.every((response) => response.ok))
})
test('sign-out stays signed out when an in-flight refresh completes', async () => {
  let release
  globalThis.fetch = async (url) =>
    url === '/api/auth/refresh'
      ? new Promise((resolve) => (release = resolve))
      : new Response(null, { status: 204 })
  const refresh = auth.refreshTokens()
  await Promise.resolve()
  await auth.logout()
  release(json({ access_token: 'late-access', refresh_token: 'late-refresh' }))
  await refresh
  assert.equal(get(auth.accessToken), '')
  assert.equal(localStorage.getItem('access_token'), null)
})

test('a late profile response cannot restore the signed-out workspace', async () => {
  let release
  globalThis.fetch = async (url) =>
    url === '/api/me'
      ? new Promise((resolve) => (release = resolve))
      : new Response(null, { status: 204 })
  const profile = auth.fetchMe()
  await auth.logout()
  release(json({ username: 'previous-user' }))
  await profile
  assert.equal(get(auth.user), null)
})
