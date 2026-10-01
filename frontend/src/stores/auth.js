import { writable, get } from 'svelte/store'
import { csrfHeaders, ensureCSRFToken } from '../lib/csrf.js'

export const user = writable(null)
export const accessToken = writable(localStorage.getItem('access_token') || '')
export const twoFAPending = writable(null) // { pending_token } wenn 2FA nötig

let refreshTimer = null
let refreshPromise = null
let sessionVersion = 0

export async function login(username, password) {
  await ensureCSRFToken()
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ username, password })
  })
  if (!res.ok) throw new Error('Invalid credentials')
  const data = await res.json()

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
  await ensureCSRFToken()
  const res = await fetch('/api/auth/2fa/verify', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ pending_token: pendingToken, code })
  })
  if (!res.ok) throw new Error('Invalid 2FA code')
  const data = await res.json()
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
  const res = await apiFetch('/api/me')
  if (res.ok) {
    const profile = await res.json()
    if (get(accessToken)) user.set(profile)
  }
}

export async function refreshTokens() {
  if (refreshPromise) return refreshPromise
  const version = sessionVersion
  refreshPromise = (async () => {
    const refresh = localStorage.getItem('refresh_token')
    if (!refresh) {
      clearTokens()
      return
    }
    await ensureCSRFToken()
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
    if (version === sessionVersion) setTokens(tokens)
  })().finally(() => {
    refreshPromise = null
  })
  return refreshPromise
}

function setTokens({ access_token, refresh_token }) {
  sessionVersion++
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
  clearTimeout(refreshTimer)
}

export async function apiFetch(url, options = {}, retry = true) {
  if (options.method && options.method !== 'GET' && options.method !== 'HEAD')
    await ensureCSRFToken()
  const token = get(accessToken)
  const res = await fetch(url, {
    ...options,
    headers: {
      ...options.headers,
      Authorization: `Bearer ${token}`,
      ...csrfHeaders()
    }
  })
  if (res.status === 401 && retry && localStorage.getItem('refresh_token')) {
    await refreshTokens()
    if (get(accessToken)) return apiFetch(url, options, false)
  }
  return res
}
