import { writable } from 'svelte/store'
import { accessToken, apiFetch, getSessionVersion, user } from './auth.js'
import { requireOK } from '../lib/files.js'

export const favorites = writable([])
let loadVersion = 0
let ownerId = null

accessToken.subscribe((token) => {
  if (!token) {
    loadVersion++
    favorites.set([])
  }
})

user.subscribe((profile) => {
  const nextOwnerId = profile?.id ?? null
  if (!profile || nextOwnerId !== ownerId) {
    loadVersion++
    favorites.set([])
  }
  ownerId = nextOwnerId
})

export async function loadFavorites() {
  const version = getSessionVersion()
  const request = ++loadVersion
  const isCurrent = () =>
    version === getSessionVersion() && request === loadVersion
  try {
    const res = await apiFetch('/api/me/favorites')
    if (!isCurrent()) return
    await requireOK(res, 'Could not load favorites')
    const list = (await res.json()) ?? []
    if (isCurrent()) favorites.set(list)
  } catch (error) {
    if (isCurrent()) throw error
  }
}

export async function addFavorite(path, name, isDir) {
  const version = getSessionVersion()
  await requireOK(
    await apiFetch('/api/me/favorites', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, name, is_dir: isDir })
    })
  )
  if (version === getSessionVersion()) await loadFavorites()
}

export async function removeFavorite(path) {
  const version = getSessionVersion()
  await requireOK(
    await apiFetch(`/api/me/favorites?path=${encodeURIComponent(path)}`, {
      method: 'DELETE'
    })
  )
  if (version === getSessionVersion()) await loadFavorites()
}
