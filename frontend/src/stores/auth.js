import { writable, get } from 'svelte/store'
import { csrfHeaders, ensureCSRFToken } from '../lib/csrf.js'

export const user = writable(null)
export const accessToken = writable(localStorage.getItem('access_token') || '')
export const twoFAPending = writable(null) // { pending_token } wenn 2FA nötig
export const activeSourceId = writable(null)
export const activeAgentId = writable(null)

let refreshTimer = null
let refreshPromise = null
let refreshSessionVersion = -1
let sessionVersion = 0

export function getSessionVersion() {
  return sessionVersion
}

export async function login(username, password) {
  const version = ++sessionVersion
  await ensureCSRFToken()
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ username, password })
  })
  if (!res.ok) throw new Error('Invalid credentials')
  const data = await res.json()
  if (version !== sessionVersion) return { requires2fa: false }

  // 2FA erforderlich
  if (data.requires_2fa) {
    twoFAPending.set({ pending_token: data.pending_token })
    return { requires2fa: true }
  }

  setTokens(data)
  await fetchMe()
  return { requires2fa: false }
}

export async function verify2FA(pendingToken, code) {
  const version = ++sessionVersion
  await ensureCSRFToken()
  const res = await fetch('/api/auth/2fa/verify', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ pending_token: pendingToken, code })
  })
  if (!res.ok) throw new Error('Invalid 2FA code')
  const data = await res.json()
  if (version !== sessionVersion) return
  twoFAPending.set(null)
  setTokens(data)
  await fetchMe()
}

export async function logout() {
  const refresh = localStorage.getItem('refresh_token')
  clearTokens()
  try {
    await ensureCSRFToken()
    await fetch('/api/auth/logout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
      body: JSON.stringify({ refresh_token: refresh })
    })
  } catch {
    /* Local sign-out still succeeds when the server is unreachable. */
  }
}

export async function fetchMe() {
  const version = sessionVersion
  const res = await apiFetch('/api/me')
  if (res.ok) {
    const profile = await res.json()
    if (version === sessionVersion && get(accessToken)) user.set(profile)
  }
}

export async function refreshTokens() {
  if (refreshPromise && refreshSessionVersion === sessionVersion)
    return refreshPromise
  const version = sessionVersion
  const promise = (async () => {
    const refresh = localStorage.getItem('refresh_token')
    if (!refresh) {
      clearTokens()
      return
    }
    await ensureCSRFToken()
    if (version !== sessionVersion) return
    const res = await fetch('/api/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
      body: JSON.stringify({ refresh_token: refresh })
    })
    if (version !== sessionVersion) return
    if (!res.ok) {
      clearTokens()
      return
    }
    const tokens = await res.json()
    if (version === sessionVersion) setTokens(tokens, false)
  })().finally(() => {
    if (refreshPromise === promise) refreshPromise = null
  })
  refreshPromise = promise
  refreshSessionVersion = version
  return promise
}

function setTokens({ access_token, refresh_token }, newSession = true) {
  if (newSession) sessionVersion++
  localStorage.setItem('access_token', access_token)
  localStorage.setItem('refresh_token', refresh_token)
  accessToken.set(access_token)
  // Auto-refresh 1 min before expiry (access token = 15min)
  clearTimeout(refreshTimer)
  refreshTimer = setTimeout(
    () => refreshTokens().catch(() => {}),
    14 * 60 * 1000
  )
}

function clearTokens() {
  sessionVersion++
  localStorage.removeItem('access_token')
  localStorage.removeItem('refresh_token')
  accessToken.set('')
  user.set(null)
  twoFAPending.set(null)
  activeSourceId.set(null)
  activeAgentId.set(null)
  clearTimeout(refreshTimer)
}

export async function apiFetch(url, options = {}, retry = true) {
  const version = sessionVersion
  const method = (options.method || 'GET').toUpperCase()
  if (method !== 'GET' && method !== 'HEAD') await ensureCSRFToken()
  if (version !== sessionVersion)
    throw new DOMException('Request cancelled', 'AbortError')
  const token = get(accessToken)
  const headers = new Headers(options.headers)
  headers.set('Authorization', `Bearer ${token}`)
  const agentId = get(activeAgentId)
  const sourceId = get(activeSourceId)
  if (agentId) headers.set('X-Agent-ID', String(agentId))
  else if (sourceId) headers.set('X-Source-ID', String(sourceId))
  for (const [name, value] of Object.entries(csrfHeaders()))
    headers.set(name, value)
  const res = await fetch(url, {
    ...options,
    headers
  })
  if (res.status === 401 && retry && version === sessionVersion) {
    // Another request may already have rotated the expired token.
    if (token !== get(accessToken) && get(accessToken))
      return apiFetch(url, options, false)
    if (localStorage.getItem('refresh_token')) {
      await refreshTokens()
      if (version === sessionVersion && get(accessToken))
        return apiFetch(url, options, false)
    }
  }
  return res
}
