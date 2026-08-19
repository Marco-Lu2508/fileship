import { writable } from 'svelte/store'
import { apiFetch } from './auth.js'

export const files = writable([])
export const currentPath = writable('')
export const loading = writable(false)
export const selected = writable(new Set())
let listController = null

// Scroll-Position pro Pfad speichern
export function saveScrollPosition(path, y) {
  try {
    const key = 'fileship_scroll'
    const map = JSON.parse(localStorage.getItem(key) || '{}')
    map[path] = y
    // Maximal 50 Pfade cachen
    const keys = Object.keys(map)
    if (keys.length > 50) delete map[keys[0]]
    localStorage.setItem(key, JSON.stringify(map))
  } catch {}
}

export function getScrollPosition(path) {
  try {
    const map = JSON.parse(localStorage.getItem('fileship_scroll') || '{}')
    return map[path] ?? 0
  } catch { return 0 }
}

export async function loadFiles(path = '') {
  listController?.abort()
  const controller = new AbortController()
  listController = controller
  loading.set(true)
  currentPath.set(path)
  selected.set(new Set())
  try {
    const res = await apiFetch(`/api/files?path=${encodeURIComponent(path)}`, { signal: controller.signal })
    if (res.ok) files.set(await res.json())
  } catch (error) {
    if (error.name !== 'AbortError') throw error
  } finally {
    if (!controller.signal.aborted) loading.set(false)
  }
}

export async function mkdir(path) {
  await apiFetch('/api/files/mkdir', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path })
  })
}

export async function deleteFile(path) {
  await apiFetch(`/api/files?path=${encodeURIComponent(path)}`, { method: 'DELETE' })
}

export async function renameFile(oldPath, newPath) {
  await apiFetch('/api/files/rename', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ old: oldPath, new: newPath })
  })
}

export async function uploadFiles(path, fileList) {
  const form = new FormData()
  form.append('path', path)
  for (const f of fileList) form.append('files', f)
  await apiFetch('/api/files/upload', { method: 'POST', body: form })
}

export async function downloadFile(path, archive = false) {
  const endpoint = archive ? '/api/files/zip' : '/api/files/download'
  const res = await apiFetch(`${endpoint}?path=${encodeURIComponent(path)}`)
  if (!res.ok) throw new Error('Download failed')
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = path.split('/').pop() + (archive ? '.zip' : '')
  link.click()
  URL.revokeObjectURL(url)
}
