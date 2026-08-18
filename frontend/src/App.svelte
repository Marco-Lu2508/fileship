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
  <div class="splash">🚀</div>
{:else if !$user}
  <Login />
{:else if route === '/admin' && $user.is_admin}
  <Admin />
{:else}
  <Browser />
{/if}

<style>
  :global(:root) {
    --bg:        #0f1117;
    --surface:   #1a1d27;
    --border:    #2a2d3a;
    --text:      #e2e8f0;
    --muted:     #718096;
    --accent:    #5865f2;
    --accent-h:  #4752c4;
    --danger:    #f87171;
    --danger-bg: #3d1515;
    --skel-bg:   #2a2d3a;
  }
  :global([data-theme="light"]:root) {
    --bg:        #f7f8fc;
    --surface:   #ffffff;
    --border:    #e2e8f0;
    --text:      #1a202c;
    --muted:     #718096;
    --accent:    #5865f2;
    --accent-h:  #4752c4;
    --danger:    #e53e3e;
    --danger-bg: #fff5f5;
    --skel-bg:   #e2e8f0;
  }
  :global(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :global(body) {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    background: var(--bg);
    color: var(--text);
  }
  :global(input, button, a) { font-family: inherit; }
  .splash {
    min-height: 100vh; display: flex; align-items: center;
    justify-content: center; font-size: 3rem; background: var(--bg);
  }
</style>
