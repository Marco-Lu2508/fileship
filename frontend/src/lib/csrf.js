export function getCSRFToken() {
  const match = document.cookie.match(/csrf_token=([^;]+)/)
  return match ? match[1] : ''
}

export function csrfHeaders() {
  return { 'X-CSRF-Token': getCSRFToken() }
}

export async function ensureCSRFToken() {
  if (getCSRFToken()) return
  // API GETs also set the cookie when Vite serves the development frontend.
  await fetch('/api/me', { credentials: 'same-origin', cache: 'no-store' })
}
