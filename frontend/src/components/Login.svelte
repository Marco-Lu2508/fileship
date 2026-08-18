<script>
  import { login } from '../stores/auth.js'

  let username = ''
  let password = ''
  let errorMsg = ''
  let loading = false

  async function handleSubmit() {
    errorMsg = ''
    loading = true
    try {
      await login(username, password)
    } catch (e) {
      errorMsg = e.message
    } finally {
      loading = false
    }
  }
</script>

<div class="wrap">
  <div class="box">
    <div class="logo">
      <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M3 9l9-7 9 7v11a2 2 0 01-2 2H5a2 2 0 01-2-2z"/>
        <polyline points="9 22 9 12 15 12 15 22"/>
      </svg>
      <span>Fileship</span>
    </div>
    <form onsubmit={(e) => { e.preventDefault(); handleSubmit() }}>
      <div class="field">
        <label for="username">Username</label>
        <input id="username" type="text" bind:value={username} autocomplete="username" required />
      </div>
      <div class="field">
        <label for="password">Password</label>
        <input id="password" type="password" bind:value={password} autocomplete="current-password" required />
      </div>
      {#if errorMsg}
        <p class="error">{errorMsg}</p>
      {/if}
      <button type="submit" disabled={loading}>
        {loading ? 'Signing in...' : 'Sign in'}
      </button>
    </form>
  </div>
</div>

<style>
  .wrap {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg);
  }
  .box {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 2rem;
    width: 100%;
    max-width: 340px;
    box-shadow: var(--shadow);
  }
  .logo {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    font-size: 1.25rem;
    font-weight: 600;
    color: var(--text);
    margin-bottom: 1.75rem;
    justify-content: center;
  }
  .logo svg { color: var(--accent); }
  .field {
    margin-bottom: 1rem;
  }
  label {
    display: block;
    font-size: 0.8rem;
    color: var(--text2);
    margin-bottom: 0.3rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  input {
    width: 100%;
    padding: 0.55rem 0.75rem;
    background: var(--input-bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text);
    font-size: 0.9rem;
  }
  input:focus { outline: none; border-color: var(--accent); }
  button {
    width: 100%;
    padding: 0.6rem;
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: 4px;
    font-size: 0.9rem;
    font-weight: 500;
    cursor: pointer;
    margin-top: 0.5rem;
  }
  button:hover:not(:disabled) { background: var(--accent-h); }
  button:disabled { opacity: 0.55; cursor: not-allowed; }
  .error { color: var(--danger); font-size: 0.82rem; margin-bottom: 0.75rem; }
</style>
