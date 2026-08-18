export function getCSRFToken() {
  const match = document.cookie.match(/csrf_token=([^;]+)/)
  return match ? match[1] : ''
}

export function csrfHeaders() {
  return { 'X-CSRF-Token': getCSRFToken() }
}
