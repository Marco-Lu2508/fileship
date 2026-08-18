<script>
  import { onMount } from 'svelte'
  import { user, fetchMe, refreshTokens } from './stores/auth.js'
  import { theme } from './stores/theme.js'
  import Login from './components/Login.svelte'
  import Browser from './components/Browser.svelte'
  import Admin from './components/Admin.svelte'

  let ready = false

  onMount(async () => {
    if (localStorage.getItem('access_token')) {
      await fetchMe()
      if (!$user) await refreshTokens().then(fetchMe)
    }
    ready = true
  })

  $: route = location.pathname
</script>

{#if !ready}
  <div class="splash"></div>
{:else if !$user}
  <Login />
{:else if route === '/admin' && $user.is_admin}
  <Admin />
{:else}
  <Browser />
{/if}

<style>
  /* ── Dark (default) ─────────────────────────────── */
  :global(:root),
  :global([data-theme="dark"]) {
    --bg:          #1a1a2e;
    --bg2:         #16213e;
    --surface:     #0f3460;
    --surface2:    #1a1a2e;
    --border:      #1f4068;
    --text:        #e0e0e0;
    --text2:       #a0a0b0;
    --accent:      #e94560;
    --accent-h:    #c73652;
    --danger:      #e94560;
    --danger-bg:   #2a1020;
    --success:     #4caf50;
    --skel:        #1f4068;
    --input-bg:    #0f3460;
    --header-bg:   #16213e;
    --row-hover:   #0f3460;
    --shadow:      0 1px 3px rgba(0,0,0,0.4);
  }

  /* ── Light ──────────────────────────────────────── */
  :global([data-theme="light"]) {
    --bg:          #f5f5f5;
    --bg2:         #ebebeb;
    --surface:     #ffffff;
    --surface2:    #f9f9f9;
    --border:      #d8d8d8;
    --text:        #1a1a1a;
    --text2:       #666666;
    --accent:      #1565c0;
    --accent-h:    #0d47a1;
    --danger:      #c62828;
    --danger-bg:   #ffebee;
    --success:     #2e7d32;
    --skel:        #e0e0e0;
    --input-bg:    #ffffff;
    --header-bg:   #ffffff;
    --row-hover:   #f0f4ff;
    --shadow:      0 1px 3px rgba(0,0,0,0.12);
  }

  /* ── Nord ───────────────────────────────────────── */
  :global([data-theme="nord"]) {
    --bg:          #2e3440;
    --bg2:         #272c36;
    --surface:     #3b4252;
    --surface2:    #2e3440;
    --border:      #434c5e;
    --text:        #eceff4;
    --text2:       #9099a8;
    --accent:      #88c0d0;
    --accent-h:    #6aacbe;
    --danger:      #bf616a;
    --danger-bg:   #3b2a2b;
    --success:     #a3be8c;
    --skel:        #434c5e;
    --input-bg:    #3b4252;
    --header-bg:   #272c36;
    --row-hover:   #3b4252;
    --shadow:      0 1px 3px rgba(0,0,0,0.3);
  }

  /* ── Solarized Dark ─────────────────────────────── */
  :global([data-theme="solarized"]) {
    --bg:          #002b36;
    --bg2:         #00212b;
    --surface:     #073642;
    --surface2:    #002b36;
    --border:      #0d4a5a;
    --text:        #839496;
    --text2:       #586e75;
    --accent:      #268bd2;
    --accent-h:    #1a6fa8;
    --danger:      #dc322f;
    --danger-bg:   #2a1010;
    --success:     #859900;
    --skel:        #0d4a5a;
    --input-bg:    #073642;
    --header-bg:   #00212b;
    --row-hover:   #073642;
    --shadow:      0 1px 3px rgba(0,0,0,0.4);
  }

  /* ── Gruvbox ────────────────────────────────────── */
  :global([data-theme="gruvbox"]) {
    --bg:          #282828;
    --bg2:         #1d2021;
    --surface:     #3c3836;
    --surface2:    #282828;
    --border:      #504945;
    --text:        #ebdbb2;
    --text2:       #a89984;
    --accent:      #d79921;
    --accent-h:    #b57614;
    --danger:      #cc241d;
    --danger-bg:   #2a1010;
    --success:     #98971a;
    --skel:        #504945;
    --input-bg:    #3c3836;
    --header-bg:   #1d2021;
    --row-hover:   #3c3836;
    --shadow:      0 1px 3px rgba(0,0,0,0.4);
  }

  :global(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :global(body) {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    background: var(--bg);
    color: var(--text);
    font-size: 14px;
    line-height: 1.5;
  }
  :global(input, button, a, select, textarea) { font-family: inherit; }
  :global(a) { color: var(--accent); }

  .splash {
    min-height: 100vh;
    background: var(--bg);
  }
</style>
