import { get } from 'svelte/store'
import { accessToken } from './auth.js'
import { loadFiles, currentPath } from './files.js'

let socket = null

export function connectWS() {
  if (socket) return
  const token = get(accessToken)
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  socket = new WebSocket(`${proto}://${location.host}/ws?token=${encodeURIComponent(token)}`)

  socket.onopen = () => {}

  socket.onmessage = (e) => {
    const event = JSON.parse(e.data)
    if (['upload', 'delete', 'mkdir', 'rename'].includes(event.type)) {
      loadFiles(get(currentPath))
    }
  }

  socket.onclose = () => {
    socket = null
    setTimeout(connectWS, 3000)
  }
}

export function disconnectWS() {
  socket?.close()
  socket = null
}
