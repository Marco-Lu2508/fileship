import { mount } from 'svelte'
import App from './App.svelte'

// PWA: installable + offline shell (SW never caches /api/*).
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {})
  })
}

const app = mount(App, { target: document.getElementById('app') })

export default app
