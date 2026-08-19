<script>
  import { onMount } from 'svelte'
  import { user, fetchMe, refreshTokens } from './stores/auth.js'
  import { theme, applyTheme } from './stores/theme.js'
  import Login from './components/Login.svelte'
  import Browser from './components/Browser.svelte'
  import Admin from './components/Admin.svelte'

  let ready = false

  onMount(async () => {
    applyTheme($theme)
    if (localStorage.getItem('access_token')) {
      await fetchMe()
      if (!$user) await refreshTokens().then(fetchMe)
    }
    ready = true
  })

  $: applyTheme($theme)
  $: route = location.pathname
</script>

{#if !ready}
  <div class="splash">
    <span class="splash-icon">
      <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75">
        <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"/>
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
  /* ── CSS Variables: Dark (default) ──────────────── */
  :global(html),
  :global(html[data-theme="dark"]) {
    --bg:          #0f1623;
    --bg2:         #152030;
    --surface:     #1a2741;
    --surface2:    #152030;
    --surface3:    #1f2f47;
    --border:      #263a57;
    --border2:     #1e3050;
    --text:        #e8edf5;
    --text2:       #8fa6bf;
    --text3:       #5a7390;
    --accent:      #4f9eff;
    --accent-h:    #3080e8;
    --accent-soft: rgba(79,158,255,0.12);
    --danger:      #f07070;
    --danger-bg:   rgba(240,112,112,0.1);
    --success:     #5ec78a;
    --success-bg:  rgba(94,199,138,0.1);
    --warning:     #f5a623;
    --skel:        #1f3050;
    --input-bg:    #111e30;
    --header-bg:   #111e2e;
    --row-hover:   #1e2f46;
    --row-active:  #1b3a5c;
    --shadow-sm:   0 1px 3px rgba(0,0,0,0.3);
    --shadow:      0 4px 16px rgba(0,0,0,0.35), 0 1px 4px rgba(0,0,0,0.2);
    --shadow-lg:   0 8px 32px rgba(0,0,0,0.5), 0 2px 8px rgba(0,0,0,0.3);
    --radius:      8px;
    --radius-lg:   12px;
    --transition:  150ms ease;
  }

  /* ── Light ──────────────────────────────────────── */
  :global(html[data-theme="light"]) {
    --bg:          #f0f2f5;
    --bg2:         #e4e7ec;
    --surface:     #ffffff;
    --surface2:    #f8f9fb;
    --surface3:    #f2f4f7;
    --border:      #dde1e8;
    --border2:     #c8cdd6;
    --text:        #1a2030;
    --text2:       #5a6478;
    --text3:       #8c96a8;
    --accent:      #1a6fe8;
    --accent-h:    #0f58c8;
    --accent-soft: rgba(26,111,232,0.1);
    --danger:      #d93025;
    --danger-bg:   rgba(217,48,37,0.08);
    --success:     #1a8a4a;
    --success-bg:  rgba(26,138,74,0.08);
    --warning:     #d97706;
    --skel:        #e4e7ec;
    --input-bg:    #ffffff;
    --header-bg:   #ffffff;
    --row-hover:   #f0f4ff;
    --row-active:  #e0eaff;
    --shadow-sm:   0 1px 2px rgba(0,0,0,0.06);
    --shadow:      0 2px 8px rgba(0,0,0,0.1);
    --shadow-lg:   0 8px 24px rgba(0,0,0,0.15);
    --radius:      8px;
    --radius-lg:   12px;
    --transition:  150ms ease;
  }

  /* ── Nord ───────────────────────────────────────── */
  :global(html[data-theme="nord"]) {
    --bg:          #242933;
    --bg2:         #1e2430;
    --surface:     #2e3440;
    --surface2:    #272c36;
    --surface3:    #313744;
    --border:      #3b4252;
    --border2:     #323844;
    --text:        #eceff4;
    --text2:       #8da0b5;
    --text3:       #6a7d90;
    --accent:      #88c0d0;
    --accent-h:    #6aacbe;
    --accent-soft: rgba(136,192,208,0.12);
    --danger:      #bf616a;
    --danger-bg:   rgba(191,97,106,0.1);
    --success:     #a3be8c;
    --success-bg:  rgba(163,190,140,0.1);
    --warning:     #ebcb8b;
    --skel:        #3b4252;
    --input-bg:    #2e3440;
    --header-bg:   #1e2430;
    --row-hover:   #353b49;
    --row-active:  #3b4557;
    --shadow-sm:   0 1px 3px rgba(0,0,0,0.3);
    --shadow:      0 4px 16px rgba(0,0,0,0.3);
    --shadow-lg:   0 8px 32px rgba(0,0,0,0.45);
    --radius:      8px;
    --radius-lg:   12px;
    --transition:  150ms ease;
  }

  /* ── Solarized Dark ─────────────────────────────── */
  :global(html[data-theme="solarized"]) {
    --bg:          #002028;
    --bg2:         #001820;
    --surface:     #063541;
    --surface2:    #002028;
    --surface3:    #0a3d4a;
    --border:      #0d4a5c;
    --border2:     #083d4a;
    --text:        #93a1a1;
    --text2:       #657b83;
    --text3:       #4a636b;
    --accent:      #268bd2;
    --accent-h:    #1a70b0;
    --accent-soft: rgba(38,139,210,0.12);
    --danger:      #dc322f;
    --danger-bg:   rgba(220,50,47,0.1);
    --success:     #859900;
    --success-bg:  rgba(133,153,0,0.1);
    --warning:     #b58900;
    --skel:        #0d4a5c;
    --input-bg:    #001820;
    --header-bg:   #001820;
    --row-hover:   #0a3d4a;
    --row-active:  #0d4a5c;
    --shadow-sm:   0 1px 3px rgba(0,0,0,0.4);
    --shadow:      0 4px 16px rgba(0,0,0,0.4);
    --shadow-lg:   0 8px 32px rgba(0,0,0,0.55);
    --radius:      8px;
    --radius-lg:   12px;
    --transition:  150ms ease;
  }

  /* ── Gruvbox ────────────────────────────────────── */
  :global(html[data-theme="gruvbox"]) {
    --bg:          #1d2021;
    --bg2:         #181a1b;
    --surface:     #282828;
    --surface2:    #1d2021;
    --surface3:    #32302f;
    --border:      #3c3836;
    --border2:     #32302f;
    --text:        #ebdbb2;
    --text2:       #a89984;
    --text3:       #7c6f64;
    --accent:      #d79921;
    --accent-h:    #b8860b;
    --accent-soft: rgba(215,153,33,0.12);
    --danger:      #cc241d;
    --danger-bg:   rgba(204,36,29,0.1);
    --success:     #98971a;
    --success-bg:  rgba(152,151,26,0.1);
    --warning:     #d65d0e;
    --skel:        #3c3836;
    --input-bg:    #1d2021;
    --header-bg:   #181a1b;
    --row-hover:   #32302f;
    --row-active:  #3c3836;
    --shadow-sm:   0 1px 3px rgba(0,0,0,0.4);
    --shadow:      0 4px 16px rgba(0,0,0,0.4);
    --shadow-lg:   0 8px 32px rgba(0,0,0,0.55);
    --radius:      8px;
    --radius-lg:   12px;
    --transition:  150ms ease;
  }

  /* ── Global Reset & Base ────────────────────────── */
  :global(*, *::before, *::after) {
    box-sizing: border-box; margin: 0; padding: 0;
  }
  :global(body) {
    font-family: -apple-system, "Segoe UI", "Inter", "Helvetica Neue", Arial, sans-serif;
    background: var(--bg);
    color: var(--text);
    font-size: 13.5px;
    line-height: 1.5;
    -webkit-font-smoothing: antialiased;
  }
  :global(input, button, a, select, textarea) {
    font-family: inherit;
    font-size: inherit;
  }
  :global(a) { color: var(--accent); text-decoration: none; }
  :global(a:hover) { text-decoration: underline; }
  :global(:focus-visible) {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  :global(*:focus:not(:focus-visible)) { outline: none; }

  /* Scrollbar */
  :global(::-webkit-scrollbar) { width: 6px; height: 6px; }
  :global(::-webkit-scrollbar-track) { background: transparent; }
  :global(::-webkit-scrollbar-thumb) { background: var(--border); border-radius: 3px; }
  :global(::-webkit-scrollbar-thumb:hover) { background: var(--text3); }

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
    0%, 100% { opacity: 0.4; }
    50%       { opacity: 1; }
  }
</style>
