<script>
  import { login } from '../stores/auth.js'

  let username = ''
  let password = ''
  let error = ''
  let loading = false

  async function handleSubmit() {
    error = ''
    loading = true
    try {
      await login(username, password)
    } catch (e) {
      error = e.message
    } finally {
      loading = false
    }
  }
</script>

<div class="login-wrap">
  <div class="login-box">
    <div class="logo">🚀 Fileship</div>
    <form onsubmit={(e) => { e.preventDefault(); handleSubmit() }}>
      <input
        type="text"
        placeholder="Username"
        bind:value={username}
        autocomplete="username"
        required
      />
      <input
        type="password"
        placeholder="Password"
        bind:value={password}
        autocomplete="current-password"
        required
      />
      {#if error}
        <p class="error">{error}</p>
      {/if}
      <button type="submit" disabled={loading}>
        {loading ? 'Signing in…' : 'Sign in'}
      </button>
    </form>
  </div>
</div>

<style>
  .login-wrap {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: #0f1117;
  }
  .login-box {
    background: #1a1d27;
    border: 1px solid #2a2d3a;
    border-radius: 12px;
    padding: 2.5rem;
    width: 100%;
    max-width: 360px;
  }
  .logo {
    font-size: 1.6rem;
    font-weight: 700;
    text-align: center;
    margin-bottom: 2rem;
    color: #fff;
  }
  input {
    width: 100%;
    padding: 0.75rem 1rem;
    margin-bottom: 1rem;
    background: #0f1117;
    border: 1px solid #2a2d3a;
    border-radius: 8px;
    color: #fff;
    font-size: 0.95rem;
    box-sizing: border-box;
  }
  input:focus { outline: none; border-color: #5865f2; }
  button {
    width: 100%;
    padding: 0.75rem;
    background: #5865f2;
    color: #fff;
    border: none;
    border-radius: 8px;
    font-size: 1rem;
    cursor: pointer;
    font-weight: 600;
  }
  button:hover:not(:disabled) { background: #4752c4; }
  button:disabled { opacity: 0.6; cursor: not-allowed; }
  .error { color: #f87171; font-size: 0.85rem; margin-bottom: 0.75rem; }
</style>
