import { writable } from 'svelte/store'

export const toasts = writable([])

let id = 0

export function toast(message, type = 'info', duration = 3500) {
  const t = { id: id++, message, type }
  toasts.update(ts => [...ts, t])
  setTimeout(() => toasts.update(ts => ts.filter(x => x.id !== t.id)), duration)
}

export const success = (msg) => toast(msg, 'success')
export const error = (msg) => toast(msg, 'error', 5000)
export const info = (msg) => toast(msg, 'info')
