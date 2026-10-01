import { writable } from 'svelte/store'
import { apiFetch } from './auth.js'
import { requireOK, saveDownload } from '../lib/files.js'

export const files = writable({ files: [], total: 0, page: 1, per_page: 100 })
export const currentPath = writable('')
export const loading = writable(false)
export const selected = writable(new Set())
export const loadError = writable('')
let listController = null
let listOptions = { page: 1, sort_by: 'name', sort_asc: true }
let listedPath = ''

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
  } catch {
    return 0
  }
}

export async function loadFiles(path = '', options = {}) {
  listController?.abort()
  const controller = new AbortController()
  listController = controller
  loading.set(true)
  loadError.set('')
  if (path !== listedPath) listOptions.page = 1
  listedPath = path
  listOptions = { ...listOptions, ...options }
  currentPath.set(path)
  selected.set(new Set())
  try {
    const query = new URLSearchParams({ path, per_page: 100, ...listOptions })
    const res = await requireOK(
      await apiFetch(`/api/files?${query}`, { signal: controller.signal }),
      'Could not load this folder'
    )
    const result = await res.json()
    if (controller.signal.aborted) return
    const lastPage = Math.max(1, Math.ceil(result.total / result.per_page))
    if (result.page > lastPage) return loadFiles(path, { page: lastPage })
    files.set({ ...result, files: result.files || [] })
  } catch (error) {
    if (!controller.signal.aborted) {
      files.set({ files: [], total: 0, page: 1, per_page: 100 })
      loadError.set(error.message || 'Could not load this folder')
    }
  } finally {
    if (!controller.signal.aborted) loading.set(false)
  }
}

export async function mkdir(path) {
  await requireOK(
    await apiFetch('/api/files/mkdir', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path })
    })
  )
}

export async function deleteFile(path) {
  await requireOK(
    await apiFetch(`/api/files?path=${encodeURIComponent(path)}`, {
      method: 'DELETE'
    })
  )
}

export async function renameFile(oldPath, newPath) {
  await requireOK(
    await apiFetch('/api/files/rename', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ old: oldPath, new: newPath })
    })
  )
}

export async function uploadFiles(path, fileList) {
  const form = new FormData()
  form.append('path', path)
  for (const f of fileList) form.append('files', f)
  await requireOK(
    await apiFetch('/api/files/upload', { method: 'POST', body: form })
  )
}

export async function downloadFile(path, archive = false) {
  const endpoint = archive ? '/api/files/zip' : '/api/files/download'
  const res = await apiFetch(`${endpoint}?path=${encodeURIComponent(path)}`)
  await saveDownload(res, path.split('/').pop() + (archive ? '.zip' : ''))
}
