<script>
  import { login, verify2FA, twoFAPending } from '../stores/auth.js'
  import Icon from './Icon.svelte'

  let username = ''
  let password = ''
  let twoFACode = ''
  let errorMsg = ''
  let loading = false
  let showPw = false

  async function handleSubmit() {
    errorMsg = ''
    loading = true
    try {
      const result = await login(username, password)
      // 2FA flow: twoFAPending store wird gesetzt, UI wechselt
    } catch (e) {
      errorMsg = e.message || 'Login failed'
    } finally {
      loading = false
    }
  }

  async function handle2FA() {
    if (!twoFACode.trim()) { errorMsg = 'Enter your 6-digit code'; return }
    errorMsg = ''
    loading = true
    try {
      const pending = $twoFAPending
      await verify2FA(pending.pending_token, twoFACode.trim())
    } catch (e) {
      errorMsg = e.message || 'Invalid code'
    } finally {
      loading = false
    }
  }

  $: is2FAStep = !!$twoFAPending
</script>

<div class="wrap">
  <div class="card">
    <div class="brand">
      <div class="brand-icon">
        <Icon name="folder" size={22} />
      </div>
      <div class="brand-text">
        <span class="brand-name">Fileship</span>
        <span class="brand-sub">Self-hosted file manager</span>
      </div>
    </div>

    <form onsubmit={(e) => { e.preventDefault(); handleSubmit() }}>
      {#if is2FAStep}
        <!-- 2FA Step -->
        <div class="twofa-header">
          <span class="twofa-icon"><Icon name="lock" size={18} /></span>
          <div>
            <strong>Two-factor authentication</strong>
            <p>Enter the 6-digit code from your authenticator app.</p>
          </div>
        </div>
        <div class="field">
          <label for="twofa-code">Authentication code</label>
          <input
            id="twofa-code"
            type="text"
            inputmode="numeric"
            maxlength="6"
            bind:value={twoFACode}
            autocomplete="one-time-code"
            required
            placeholder="000000"
            class="code-input"
          />
        </div>
        {#if errorMsg}
          <div class="error-msg"><Icon name="warning" size={13} /><span>{errorMsg}</span></div>
        {/if}
        <button type="button" class="submit-btn" onclick={handle2FA} disabled={loading}>
          {#if loading}<span class="btn-spinner"></span> Verifying…
          {:else}Verify{/if}
        </button>
        <button type="button" class="back-link" onclick={() => twoFAPending.set(null)}>
          ← Back to login
        </button>
      {:else}
        <!-- Normal login -->
        <div class="field">
          <label for="username">Username</label>
          <div class="input-wrap">
            <span class="input-icon"><Icon name="user" size={14} /></span>
            <input id="username" type="text" bind:value={username}
              autocomplete="username" required placeholder="admin" />
          </div>
        </div>
        <div class="field">
          <label for="password">Password</label>
          <div class="input-wrap">
            <span class="input-icon"><Icon name="lock" size={14} /></span>
            <input id="password" type={showPw ? 'text' : 'password'} bind:value={password}
              autocomplete="current-password" required placeholder="••••••••" />
            <button type="button" class="pw-toggle" onclick={() => showPw = !showPw}
              aria-label="Toggle password visibility">
              <Icon name={showPw ? 'eye' : 'eye'} size={14} />
            </button>
          </div>
        </div>
        {#if errorMsg}
          <div class="error-msg"><Icon name="warning" size={13} /><span>{errorMsg}</span></div>
        {/if}
        <button type="submit" class="submit-btn" disabled={loading}>
          {#if loading}<span class="btn-spinner"></span> Signing in…
          {:else}Sign in{/if}
        </button>
      {/if}
    </form>
  </div>

  <p class="footer-note">Fileship · Self-hosted · MIT</p>
</div>

<style>
  .wrap {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    background: var(--bg);
    padding: 1.5rem;
    gap: 1rem;
  }

  .card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 2.25rem 2rem;
    width: 100%;
    max-width: 360px;
    box-shadow: var(--shadow-lg);
    animation: card-in 0.25s cubic-bezier(.2,.8,.3,1) both;
  }

  @keyframes card-in {
    from { opacity: 0; transform: translateY(16px) scale(0.97); }
    to   { opacity: 1; transform: none; }
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 0.85rem;
    margin-bottom: 2rem;
  }

  .brand-icon {
    width: 44px;
    height: 44px;
    border-radius: var(--radius);
    background: var(--accent);
    display: grid;
    place-items: center;
    color: #fff;
    flex-shrink: 0;
    box-shadow: 0 2px 8px rgba(79,158,255,0.4);
  }

  .brand-text {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .brand-name {
    font-size: 1.15rem;
    font-weight: 700;
    color: var(--text);
    letter-spacing: -0.02em;
  }

  .brand-sub {
    font-size: 0.75rem;
    color: var(--text2);
  }

  form {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  label {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text2);
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .input-wrap {
    position: relative;
    display: flex;
    align-items: center;
  }

  .input-icon {
    position: absolute;
    left: 0.7rem;
    color: var(--text3);
    display: flex;
    pointer-events: none;
  }

  input {
    width: 100%;
    padding: 0.65rem 0.75rem 0.65rem 2.2rem;
    background: var(--input-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--text);
    font-size: 0.9rem;
    transition: border-color var(--transition), box-shadow var(--transition);
  }

  input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  .pw-toggle {
    position: absolute;
    right: 0.6rem;
    background: none;
    border: none;
    color: var(--text3);
    cursor: pointer;
    display: flex;
    padding: 0.2rem;
    border-radius: 4px;
  }

  .pw-toggle:hover { color: var(--text2); }

  .error-msg {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    background: var(--danger-bg);
    border: 1px solid var(--danger);
    color: var(--danger);
    font-size: 0.8rem;
    padding: 0.5rem 0.75rem;
    border-radius: var(--radius);
    animation: shake 0.35s ease;
  }

  @keyframes shake {
    0%, 100% { transform: none; }
    20%, 60%  { transform: translateX(-4px); }
    40%, 80%  { transform: translateX(4px); }
  }

  .submit-btn {
    width: 100%;
    padding: 0.7rem;
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: var(--radius);
    font-size: 0.9rem;
    font-weight: 600;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    transition: background var(--transition), transform var(--transition), box-shadow var(--transition);
    box-shadow: 0 2px 8px rgba(79,158,255,0.3);
    margin-top: 0.25rem;
  }

  .submit-btn:hover:not(:disabled) {
    background: var(--accent-h);
    box-shadow: 0 4px 14px rgba(79,158,255,0.45);
  }

  .submit-btn:active:not(:disabled) {
    transform: scale(0.98);
  }

  .submit-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .btn-spinner {
    width: 14px;
    height: 14px;
    border: 2px solid rgba(255,255,255,0.4);
    border-top-color: #fff;
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }

  @keyframes spin { to { transform: rotate(360deg); } }

  .footer-note {
    font-size: 0.75rem;
    color: var(--text3);
  }

  /* 2FA step */
  .twofa-header {
    display: flex; align-items: flex-start; gap: 0.75rem;
    background: var(--accent-soft); border: 1px solid rgba(79,158,255,0.25);
    border-radius: var(--radius); padding: 0.85rem;
  }
  .twofa-icon {
    width: 36px; height: 36px; border-radius: var(--radius);
    background: var(--accent); color: #fff;
    display: grid; place-items: center; flex-shrink: 0;
  }
  .twofa-header strong { font-size: 0.9rem; display: block; color: var(--text); }
  .twofa-header p { font-size: 0.78rem; color: var(--text2); margin-top: 0.15rem; }
  .code-input {
    text-align: center; letter-spacing: 0.4rem;
    font-size: 1.4rem; font-family: monospace;
    padding: 0.65rem 0.75rem 0.65rem 2.2rem;
  }
  .back-link {
    background: none; border: none; color: var(--text2);
    font-size: 0.8rem; cursor: pointer; padding: 0;
    text-align: center; width: 100%;
    transition: color var(--transition);
  }
  .back-link:hover { color: var(--accent); }
</style>
