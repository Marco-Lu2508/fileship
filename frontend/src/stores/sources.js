import { writable, get } from 'svelte/store'
import { apiFetch, activeSourceId, activeAgentId } from './auth.js'

export { activeSourceId, activeAgentId }
export const sources = writable([])
export const agents = writable([])

export async function loadSources() {
  try {
    const res = await apiFetch('/api/me/sources')
    if (res.ok && res.status !== 204) {
      const list = (await res.json()) ?? []
      sources.set(list)
      if (list.length > 0 && !get(activeAgentId)) {
        activeSourceId.update((cur) => {
          if (cur) return cur
          const def = list.find((s) => s.is_default)
          return def?.id ?? null
        })
      }
    }
  } catch {}
}

export async function loadAgents() {
  try {
    const res = await apiFetch('/api/me/agents')
    if (res.ok && res.status !== 204) {
      const list = (await res.json()) ?? []
      agents.set(list)
    }
  } catch {}
}

export function selectLocalWorkspace(sourceId = null) {
  activeAgentId.set(null)
  activeSourceId.set(sourceId)
}

export function selectRemoteAgent(agentId) {
  activeSourceId.set(null)
  activeAgentId.set(agentId)
}

export function getSourceParam(
  sourceId = get(activeSourceId),
  agentId = get(activeAgentId)
) {
  if (agentId) return `&agent_id=${encodeURIComponent(agentId)}`
  if (sourceId) return `&source_id=${encodeURIComponent(sourceId)}`
  return ''
}
