import { test, beforeEach, afterEach } from 'node:test'
import assert from 'node:assert/strict'
import { get } from 'svelte/store'
import { locale } from '../src/stores/i18n.js'
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
const favoritesStore = await import('../src/stores/favorites.js')
const json = (value) =>
  new Response(JSON.stringify(value), {
    headers: { 'Content-Type': 'application/json' }
  })

beforeEach(() => {
  locale.set('en')
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
  assert.equal(formatBytes(1536, 'de'), '1,5 KB')
  assert.equal(formatBytes(1536, 'en'), '1.5 KB')
  assert.equal(
    fileKind({ name: 'report.PDF', mime_type: 'application/pdf' }),
    'text'
  )
  assert.equal(fileKind({ name: 'photo.png', mime_type: '' }), 'image')
  assert.equal(fileKind({ name: 'clip.mp4', mime_type: '' }), 'video')
  assert.equal(fileKind({ name: 'song.mp3', mime_type: '' }), 'audio')
  assert.equal(fileKind({ name: 'main.go', mime_type: '' }), 'text')
  for (const name of ['', '  ', '..', 'a/b', 'a\\b', 'bad\nname'])
    assert.equal(validName(name), false)
})
test('failed mutations propagate server errors instead of reporting success', async () => {
  globalThis.fetch = async () =>
    new Response('permission denied', { status: 403 })
  await assert.rejects(workspace.mkdir('restricted'), /permission denied/i)
  await assert.rejects(workspace.deleteFile('restricted'), /permission denied/i)
  await assert.rejects(workspace.renameFile('a', 'b'), /permission denied/i)
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
test('live refresh preserves selected files, removes deleted files and keeps search selections', async () => {
  let entries = [
    { path: 'selection/kept.txt' },
    { path: 'selection/deleted.txt' }
  ]
  globalThis.fetch = async () =>
    json({ files: entries, total: entries.length, page: 1, per_page: 100 })
  await workspace.loadFiles('selection', {
    page: 1,
    sort_by: 'name',
    sort_asc: true
  })
  workspace.selected.set(
    new Set([
      'selection/kept.txt',
      'selection/deleted.txt',
      'another-folder/search-result.txt'
    ])
  )
  entries = [{ path: 'selection/kept.txt' }, { path: 'selection/new.txt' }]
  await workspace.loadFiles('selection')
  assert.deepEqual(
    [...get(workspace.selected)],
    ['selection/kept.txt', 'another-folder/search-result.txt']
  )
})
test('changing the folder, page or sort order clears the previous selection', async () => {
  globalThis.fetch = async (url) => {
    const page = Number(
      new URL(url, 'http://localhost').searchParams.get('page')
    )
    return json({
      files: [{ path: `selection/page-${page}.txt` }],
      total: 300,
      page,
      per_page: 100
    })
  }
  await workspace.loadFiles('selection-scope', {
    page: 1,
    sort_by: 'name',
    sort_asc: true
  })
  for (const [path, options] of [
    ['selection-scope', { page: 2 }],
    ['selection-scope', { sort_by: 'size' }],
    ['selection-scope', { sort_asc: false }],
    ['different-folder', {}]
  ]) {
    workspace.selected.set(new Set(['previous-file.txt']))
    await workspace.loadFiles(path, options)
    assert.equal(get(workspace.selected).size, 0)
  }
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
    return new Headers(options.headers).get('Authorization') ===
      'Bearer new-access'
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

test('request headers support Headers instances and tuple arrays', async () => {
  const requests = []
  globalThis.fetch = async (_url, options) => {
    requests.push(new Headers(options.headers))
    return json({ ok: true })
  }
  await auth.apiFetch('/api/files', {
    headers: new Headers({
      'If-Match': 'current-version',
      authorization: 'Bearer stale',
      'x-csrf-token': 'stale-csrf'
    })
  })
  await auth.apiFetch('/api/files', {
    headers: [['X-Request-ID', 'request-id']]
  })
  assert.equal(requests[0].get('If-Match'), 'current-version')
  assert.equal(requests[1].get('X-Request-ID'), 'request-id')
  assert.equal(requests[0].get('Authorization'), 'Bearer old-access')
  assert.equal(requests[0].get('X-CSRF-Token'), 'test-csrf')
})

test('a delayed unauthorized response reuses an already refreshed token', async () => {
  let release
  let refreshes = 0
  globalThis.fetch = async (url, options) => {
    if (url === '/api/auth/refresh') {
      refreshes++
      return json({ access_token: 'new-access', refresh_token: 'new-refresh' })
    }
    if (
      new Headers(options.headers).get('Authorization') === 'Bearer new-access'
    )
      return json({ ok: true })
    if (url === '/slow') return new Promise((resolve) => (release = resolve))
    return new Response(null, { status: 401 })
  }
  const slow = auth.apiFetch('/slow')
  assert.equal((await auth.apiFetch('/fast')).ok, true)
  release(new Response(null, { status: 401 }))
  assert.equal((await slow).ok, true)
  assert.equal(refreshes, 1)
})

test('profile loading still succeeds when its request refreshes the session token', async () => {
  globalThis.fetch = async (url, options) => {
    if (url === '/api/auth/refresh')
      return json({ access_token: 'new-access', refresh_token: 'new-refresh' })
    return new Headers(options.headers).get('Authorization') ===
      'Bearer new-access'
      ? json({ username: 'current-user' })
      : new Response(null, { status: 401 })
  }
  await auth.fetchMe()
  assert.equal(get(auth.user).username, 'current-user')
})

test('a previous account profile cannot replace the newly signed-in account', async () => {
  let release
  let profileRequests = 0
  globalThis.fetch = async (url) => {
    if (url === '/api/auth/login')
      return json({ access_token: 'new-access', refresh_token: 'new-refresh' })
    if (url === '/api/me' && profileRequests++ === 0)
      return new Promise((resolve) => (release = resolve))
    return json({ username: 'new-user' })
  }
  const previousProfile = auth.fetchMe()
  await auth.login('new-user', 'password')
  release(json({ username: 'previous-user' }))
  await previousProfile
  assert.equal(get(auth.user).username, 'new-user')
})

test('an unauthorized mutation is never replayed under a different account', async () => {
  let release
  let mutations = 0
  let refreshes = 0
  globalThis.fetch = async (url) => {
    if (url === '/api/files/touch') {
      mutations++
      return new Promise((resolve) => (release = resolve))
    }
    if (url === '/api/auth/login')
      return json({ access_token: 'new-access', refresh_token: 'new-refresh' })
    if (url === '/api/auth/refresh') refreshes++
    return json({ username: 'new-user' })
  }
  const mutation = auth.apiFetch('/api/files/touch', { method: 'POST' })
  await Promise.resolve()
  await auth.login('new-user', 'password')
  release(new Response(null, { status: 401 }))
  assert.equal((await mutation).status, 401)
  assert.equal(mutations, 1)
  assert.equal(refreshes, 0)
})

test('a previous account refresh does not block the new account refresh', async () => {
  let release
  const refreshedTokens = []
  globalThis.fetch = async (url, options) => {
    if (url === '/api/auth/login')
      return json({ access_token: 'new-access', refresh_token: 'new-refresh' })
    if (url === '/api/auth/refresh') {
      const token = JSON.parse(options.body).refresh_token
      refreshedTokens.push(token)
      if (token === 'old-refresh')
        return new Promise((resolve) => (release = resolve))
      return json({
        access_token: 'latest-access',
        refresh_token: 'latest-refresh'
      })
    }
    return json({ username: 'new-user' })
  }
  const previousRefresh = auth.refreshTokens()
  await Promise.resolve()
  await auth.login('new-user', 'password')
  await auth.refreshTokens()
  release(json({ access_token: 'late-access', refresh_token: 'late-refresh' }))
  await previousRefresh
  assert.deepEqual(refreshedTokens, ['old-refresh', 'new-refresh'])
  assert.equal(get(auth.accessToken), 'latest-access')
  assert.equal(localStorage.getItem('refresh_token'), 'latest-refresh')
})

test('sign-out cancels a pending sign-in before it can restore tokens', async () => {
  let release
  globalThis.fetch = async (url) =>
    url === '/api/auth/login'
      ? new Promise((resolve) => (release = resolve))
      : new Response(null, { status: 204 })
  const signIn = auth.login('user', 'password')
  await Promise.resolve()
  await auth.logout()
  release(json({ access_token: 'late-access', refresh_token: 'late-refresh' }))
  await signIn
  assert.equal(get(auth.accessToken), '')
  assert.equal(get(auth.user), null)
  assert.equal(localStorage.getItem('refresh_token'), null)
})

test('favorite loading exposes server failures to the caller', async () => {
  globalThis.fetch = async () =>
    new Response('favorites unavailable', { status: 500 })
  await assert.rejects(favoritesStore.loadFavorites(), /favorites unavailable/)
})

test('favorite loading keeps the latest response when requests finish out of order', async () => {
  let release
  let calls = 0
  globalThis.fetch = async () =>
    calls++ === 0
      ? new Promise((resolve) => (release = resolve))
      : json([{ path: 'current.txt' }])
  const previous = favoritesStore.loadFavorites()
  await favoritesStore.loadFavorites()
  release(json([{ path: 'previous.txt' }]))
  await previous
  assert.deepEqual(get(favoritesStore.favorites), [{ path: 'current.txt' }])
})

test('sign-out clears favorites and ignores a late favorite response', async () => {
  let release
  favoritesStore.favorites.set([{ path: 'private.txt' }])
  globalThis.fetch = async (url) =>
    url === '/api/me/favorites'
      ? new Promise((resolve) => (release = resolve))
      : new Response(null, { status: 204 })
  const pending = favoritesStore.loadFavorites()
  await auth.logout()
  assert.deepEqual(get(favoritesStore.favorites), [])
  release(json([{ path: 'late-private.txt' }]))
  await pending
  assert.deepEqual(get(favoritesStore.favorites), [])
})

test('late favorite responses cannot overwrite another account favorites', async () => {
  let release
  let requests = 0
  globalThis.fetch = async (url) => {
    if (url === '/api/auth/login')
      return json({ access_token: 'new-access', refresh_token: 'new-refresh' })
    if (url === '/api/me') return json({ id: 2, username: 'new-user' })
    if (requests++ === 0) return new Promise((resolve) => (release = resolve))
    return json([{ path: 'new-account.txt' }])
  }
  const previous = favoritesStore.loadFavorites()
  await auth.login('new-user', 'password')
  await favoritesStore.loadFavorites()
  release(json([{ path: 'old-account.txt' }]))
  await previous
  assert.deepEqual(get(favoritesStore.favorites), [{ path: 'new-account.txt' }])
})
