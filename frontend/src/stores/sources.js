import { writable } from 'svelte/store'
import { apiFetch } from './auth.js'

export const sources = writable([])
export const activeSourceId = writable(null)

export async function loadSources() {
  const res = await apiFetch('/api/me/sources')
  if (res.ok) {
    const list = await res.json() ?? []
    sources.set(list)
    // Default-Source aktivieren falls noch keine aktiv
    if (list.length > 0) {
      activeSourceId.update(cur => {
        if (cur) return cur
        const def = list.find(s => s.is_default) ?? list[0]
        return def?.id ?? null
      })
    }
  }
}

export function getSourceParam(sourceId) {
  if (!sourceId) return ''
  return `&source_id=${sourceId}`
}
