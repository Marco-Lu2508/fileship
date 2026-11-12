import { mount } from 'svelte'
import App from './App.svelte'
import { handleSsoCallback } from './stores/auth.js'

// OIDC: consume tokens / pending / errors from the provider redirect.
const ssoError = handleSsoCallback()
if (ssoError) sessionStorage.setItem('sso_error', ssoError)

// PWA: installable + offline shell (SW never caches /api/*).
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {})
  })
}

const app = mount(App, { target: document.getElementById('app') })

export default app
