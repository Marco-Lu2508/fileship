import { get, writable } from 'svelte/store'
import { accessToken, refreshTokens } from './auth.js'
import { loadFiles, currentPath } from './files.js'

export const connection = writable('disconnected')
let socket = null,
  reconnectTimer = null,
  reloadTimer = null,
  active = false
const changes = new Set([
  'upload',
  'delete',
  'mkdir',
  'rename',
  'move',
  'copy',
  'create',
  'edit',
  'unzip',
  'restore'
])

export function connectWS() {
  active = true
  openSocket()
}

function openSocket() {
  if (!active || socket || !get(accessToken)) return
  connection.set('connecting')
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const current = new WebSocket(
    `${proto}://${location.host}/ws?token=${encodeURIComponent(get(accessToken))}`
  )
  socket = current
  current.onopen = () => {
    if (active) connection.set('connected')
  }
  current.onmessage = (message) => {
    try {
      if (!changes.has(JSON.parse(message.data).type)) return
      clearTimeout(reloadTimer)
      reloadTimer = setTimeout(() => {
        if (active) loadFiles(get(currentPath))
      }, 200)
    } catch {
      /* Ignore malformed events. */
    }
  }
  current.onclose = () => {
    if (socket === current) socket = null
    connection.set('disconnected')
    if (active)
      reconnectTimer = setTimeout(async () => {
        try {
          await refreshTokens()
        } catch {}
        if (active) openSocket()
      }, 5000)
  }
}

export function disconnectWS() {
  active = false
  clearTimeout(reconnectTimer)
  clearTimeout(reloadTimer)
  if (socket) {
    socket.onclose = null
    socket.close()
    socket = null
  }
  connection.set('disconnected')
}
