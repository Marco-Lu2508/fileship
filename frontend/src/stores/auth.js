import { writable, get } from 'svelte/store'
import { csrfHeaders, ensureCSRFToken } from '../lib/csrf.js'

export const user = writable(null)
export const accessToken = writable(localStorage.getItem('access_token') || '')

let refreshTimer = null

export async function login(username, password) {
  await ensureCSRFToken()
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ username, password })
  })
  if (!res.ok) throw new Error('Invalid credentials')
  const tokens = await res.json()
  setTokens(tokens)
  await fetchMe()
}

export async function logout() {
  const refresh = localStorage.getItem('refresh_token')
  await fetch('/api/auth/logout', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refresh })
  })
  clearTokens()
}

export async function fetchMe() {
  const res = await apiFetch('/api/me')
  if (res.ok) user.set(await res.json())
}

export async function refreshTokens() {
  const refresh = localStorage.getItem('refresh_token')
  if (!refresh) { clearTokens(); return }
  const res = await fetch('/api/auth/refresh', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refresh })
  })
  if (!res.ok) { clearTokens(); return }
  setTokens(await res.json())
}

function setTokens({ access_token, refresh_token }) {
  localStorage.setItem('access_token', access_token)
  localStorage.setItem('refresh_token', refresh_token)
  accessToken.set(access_token)
  // Auto-refresh 1 min before expiry (access token = 15min)
  clearTimeout(refreshTimer)
  refreshTimer = setTimeout(refreshTokens, 14 * 60 * 1000)
}

function clearTokens() {
  localStorage.removeItem('access_token')
  localStorage.removeItem('refresh_token')
  accessToken.set('')
  user.set(null)
  clearTimeout(refreshTimer)
}

export async function apiFetch(url, options = {}, retry = true) {
  if (options.method && options.method !== 'GET' && options.method !== 'HEAD') await ensureCSRFToken()
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
