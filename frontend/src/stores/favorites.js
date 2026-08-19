import { writable } from 'svelte/store'
import { apiFetch } from './auth.js'

export const favorites = writable([])

export async function loadFavorites() {
  const res = await apiFetch('/api/me/favorites')
  if (res.ok) favorites.set(await res.json() ?? [])
}

export async function addFavorite(path, name, isDir) {
  await apiFetch('/api/me/favorites', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path, name, is_dir: isDir })
  })
  await loadFavorites()
}

export async function removeFavorite(path) {
  await apiFetch(`/api/me/favorites?path=${encodeURIComponent(path)}`, { method: 'DELETE' })
  await loadFavorites()
}
