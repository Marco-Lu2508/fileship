<script>
  import { onMount } from 'svelte'
  import './styles/themes.css'
  import { user, fetchMe, refreshTokens } from './stores/auth.js'
  import { theme, applyTheme } from './stores/theme.js'
  import Login from './components/Login.svelte'
  import Browser from './components/Browser.svelte'
  import Admin from './components/Admin.svelte'

  let ready = false

  onMount(async () => {
    applyTheme($theme)
    try {
      if (
        localStorage.getItem('access_token') ||
        localStorage.getItem('refresh_token')
      ) {
        await fetchMe()
        if (!$user) await refreshTokens().then(fetchMe)
      }
    } catch {
      /* Show the login form if the saved session cannot be restored. */
    } finally {
      ready = true
    }
  })

  $: applyTheme($theme)
  $: route = location.pathname
</script>

{#if !ready}
  <div class="splash">
    <span class="splash-icon">
      <svg
        width="28"
        height="28"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.75"
      >
        <path
          d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"
        />
      </svg>
    </span>
  </div>
{:else if !$user}
  <Login />
{:else if route === '/admin' && $user.is_admin}
  <Admin />
{:else}
  <Browser />
{/if}

<style>
  /* ── Global Reset & Base ────────────────────────── */
  :global(*, *::before, *::after) {
    box-sizing: border-box;
    margin: 0;
    padding: 0;
  }
  :global(body) {
    font-family:
      'IBM Plex Sans', 'Segoe UI Variable', 'Segoe UI', Arial, sans-serif;
    background: var(--bg);
    color: var(--text);
    font-size: 14px;
    line-height: 1.5;
    -webkit-font-smoothing: antialiased;
  }
  :global(input[type='checkbox']) {
    accent-color: var(--accent);
    width: 14px;
    height: 14px;
    cursor: pointer;
  }
  :global(button, a, input, select) {
    -webkit-tap-highlight-color: transparent;
    touch-action: manipulation;
  }
  :global(.sr-only) {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
  @media (prefers-reduced-motion: reduce) {
    :global(*),
    :global(*::before),
    :global(*::after) {
      animation-duration: 0.01ms !important;
      animation-iteration-count: 1 !important;
      transition-duration: 0.01ms !important;
      scroll-behavior: auto !important;
    }
  }
  :global(input, button, a, select, textarea) {
    font-family: inherit;
    font-size: inherit;
  }
  :global(a) {
    color: var(--accent);
    text-decoration: none;
  }
  :global(a:hover) {
    text-decoration: underline;
  }
  :global(:focus-visible) {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  :global(*:focus:not(:focus-visible)) {
    outline: none;
  }

  /* Scrollbar */
  :global(::-webkit-scrollbar) {
    width: 6px;
    height: 6px;
  }
  :global(::-webkit-scrollbar-track) {
    background: transparent;
  }
  :global(::-webkit-scrollbar-thumb) {
    background: var(--border);
    border-radius: 3px;
  }
  :global(::-webkit-scrollbar-thumb:hover) {
    background: var(--text3);
  }

  /* ── Splash ─────────────────────────────────────── */
  .splash {
    min-height: 100vh;
    background: var(--bg);
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--accent);
    animation: pulse-splash 1.4s ease-in-out infinite;
  }
  @keyframes pulse-splash {
    0%,
    100% {
      opacity: 0.4;
    }
    50% {
      opacity: 1;
    }
  }
</style>
