<script>
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import Icon from './Icon.svelte'

  export let onClose = () => {}

  let step = 'status'   // 'status' | 'setup' | 'verify' | 'done' | 'disable'
  let enabled = false
  let secret = ''
  let qrUri = ''
  let code = ''
  let backupCodes = []
  let disablePassword = ''
  let loading = false

  $: if (step === 'status') loadStatus()

  async function loadStatus() {
    const res = await apiFetch('/api/me/2fa/status')
    if (res.ok) {
      const d = await res.json()
      enabled = d.enabled
    }
  }

  async function startSetup() {
    loading = true
    const res = await apiFetch('/api/me/2fa/setup', { method: 'POST' })
    loading = false
    if (!res.ok) { error('Setup failed'); return }
    const d = await res.json()
    secret = d.secret
    qrUri = d.provisioning_uri
    step = 'setup'
  }

  async function verifyAndEnable() {
    if (!code.trim()) { error('Enter the 6-digit code from your app'); return }
    loading = true
    const res = await apiFetch('/api/me/2fa/enable', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code: code.trim() })
    })
    loading = false
    if (!res.ok) { error('Invalid code – try again'); return }
    const d = await res.json()
    backupCodes = d.backup_codes
    enabled = true
    step = 'done'
    success('Two-factor authentication enabled!')
  }

  async function disable2FA() {
    if (!disablePassword) { error('Enter your password'); return }
    loading = true
    const res = await apiFetch('/api/me/2fa', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: disablePassword })
    })
    loading = false
    if (!res.ok) { error(await res.text()); return }
    enabled = false
    disablePassword = ''
    step = 'status'
    success('Two-factor authentication disabled')
  }

  // QR-Code URL für Google Charts API (no external call – use local canvas)
  // Wir zeigen den URI als Text + Anleitung
  function handleKey(e) { if (e.key === 'Escape') onClose() }
</script>

<svelte:window onkeydown={handleKey} />

<div class="overlay" role="dialog" aria-modal="true" tabindex="-1"
  onclick={(e) => e.target === e.currentTarget && onClose()}
  onkeydown={handleKey}>
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title">
        <span class="title-icon"><Icon name="lock" size={15} /></span>
        <strong>Two-Factor Authentication</strong>
      </div>
      <button class="close-btn" onclick={onClose}><Icon name="x" size={14} /></button>
    </div>

    <div class="modal-body">

      <!-- Status / Entry -->
      {#if step === 'status'}
        {#if enabled}
          <div class="status-badge enabled">
            <Icon name="check" size={15} /> 2FA is enabled
          </div>
          <p class="hint">Your account is protected with two-factor authentication.</p>
          <button class="btn-danger" onclick={() => step = 'disable'}>
            <Icon name="trash" size={14} /> Disable 2FA…
          </button>
        {:else}
          <div class="status-badge disabled">
            <Icon name="warning" size={15} /> 2FA is not enabled
          </div>
          <p class="hint">Add a second layer of security using a TOTP app like Google Authenticator, Authy, or 1Password.</p>
          <button class="btn-primary" onclick={startSetup} disabled={loading}>
            {loading ? 'Setting up…' : 'Enable 2FA'}
          </button>
        {/if}

      <!-- Setup: show secret + URI -->
      {:else if step === 'setup'}
        <p class="step-label">Step 1 — Scan with your authenticator app</p>
        <p class="hint">Open your TOTP app (Authy, Google Authenticator, 1Password, …) and add a new account. Use the manual entry if you can't scan a QR code.</p>

        <div class="secret-box">
          <span class="secret-label">Manual entry key</span>
          <div class="secret-val">
            <code>{secret}</code>
            <button onclick={() => { navigator.clipboard.writeText(secret); success('Copied!') }}>
              <Icon name="clipboard" size={13} />
            </button>
          </div>
        </div>

        <div class="uri-box">
          <span class="secret-label">Or copy the full URI for manual import</span>
          <div class="secret-val">
            <code class="uri">{qrUri}</code>
            <button onclick={() => { navigator.clipboard.writeText(qrUri); success('URI copied!') }}>
              <Icon name="clipboard" size={13} />
            </button>
          </div>
        </div>

        <p class="step-label" style="margin-top:1rem">Step 2 — Enter the 6-digit code to confirm</p>
        <input
          type="text"
          inputmode="numeric"
          maxlength="6"
          placeholder="000000"
          class="code-input"
          bind:value={code}
          onkeydown={(e) => e.key === 'Enter' && verifyAndEnable()}
        />
        <button class="btn-primary" onclick={verifyAndEnable} disabled={loading || code.length < 6}>
          {loading ? 'Verifying…' : 'Enable 2FA'}
        </button>

      <!-- Done: show backup codes -->
      {:else if step === 'done'}
        <div class="done-icon">
          <Icon name="check" size={22} />
        </div>
        <p class="done-title">2FA Enabled!</p>
        <div class="backup-section">
          <p class="backup-warn">
            <Icon name="warning" size={13} /> Save these backup codes in a safe place. Each can be used once if you lose access to your authenticator app.
          </p>
          <div class="backup-grid">
            {#each backupCodes as c}
              <code class="backup-code">{c}</code>
            {/each}
          </div>
          <button class="btn-secondary" onclick={() => navigator.clipboard.writeText(backupCodes.join('\n')).then(() => success('Codes copied!'))}>
            <Icon name="clipboard" size={13} /> Copy all codes
          </button>
        </div>
        <button class="btn-primary" onclick={onClose}>Done</button>

      <!-- Disable -->
      {:else if step === 'disable'}
        <p class="hint">Enter your password to disable two-factor authentication.</p>
        <input type="password" placeholder="Current password" bind:value={disablePassword}
          onkeydown={(e) => e.key === 'Enter' && disable2FA()} />
        <div class="action-row">
          <button class="btn-danger" onclick={disable2FA} disabled={loading}>
            {loading ? 'Disabling…' : 'Disable 2FA'}
          </button>
          <button class="btn-secondary" onclick={() => step = 'status'}>Cancel</button>
        </div>
      {/if}

    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; background: rgba(0,0,0,0.65);
    display: flex; align-items: center; justify-content: center;
    z-index: 800; padding: 1rem; backdrop-filter: blur(2px);
  }
  .modal {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-lg); width: 100%; max-width: 480px;
    overflow: hidden; box-shadow: var(--shadow-lg);
    animation: modal-in 0.2s cubic-bezier(.2,.8,.3,1) both;
  }
  @keyframes modal-in {
    from { opacity: 0; transform: scale(0.96) translateY(8px); }
    to   { opacity: 1; transform: none; }
  }
  .modal-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 1rem 1.1rem; border-bottom: 1px solid var(--border);
  }
  .modal-title { display: flex; align-items: center; gap: 0.65rem; }
  .modal-title strong { font-size: 0.95rem; }
  .title-icon {
    width: 32px; height: 32px; border-radius: var(--radius);
    background: var(--accent-soft); color: var(--accent);
    display: grid; place-items: center; flex-shrink: 0;
  }
  .close-btn {
    background: none; border: none; color: var(--text2);
    cursor: pointer; padding: 0.35rem; border-radius: var(--radius); display: flex;
  }
  .close-btn:hover { background: var(--row-hover); }
  .modal-body {
    padding: 1.25rem; display: flex; flex-direction: column; gap: 0.9rem;
    max-height: calc(90vh - 80px); overflow-y: auto;
  }
  .status-badge {
    display: inline-flex; align-items: center; gap: 0.4rem;
    padding: 0.5rem 0.85rem; border-radius: var(--radius);
    font-size: 0.85rem; font-weight: 600; border: 1px solid;
  }
  .status-badge.enabled { background: var(--success-bg); color: var(--success); border-color: var(--success); }
  .status-badge.disabled { background: var(--danger-bg); color: var(--danger); border-color: var(--danger); }
  .hint { font-size: 0.82rem; color: var(--text2); line-height: 1.5; }
  .step-label { font-size: 0.75rem; font-weight: 700; color: var(--text3); text-transform: uppercase; letter-spacing: 0.07em; }
  .secret-box, .uri-box {
    background: var(--surface2); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 0.75rem;
    display: flex; flex-direction: column; gap: 0.35rem;
  }
  .secret-label { font-size: 0.72rem; color: var(--text3); }
  .secret-val {
    display: flex; align-items: center; gap: 0.4rem;
    background: var(--input-bg); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 0.4rem 0.6rem;
  }
  .secret-val code { flex: 1; font-size: 0.85rem; color: var(--accent); font-family: monospace; letter-spacing: 0.1em; word-break: break-all; }
  .secret-val .uri { letter-spacing: 0; font-size: 0.72rem; }
  .secret-val button { background: none; border: none; color: var(--text2); cursor: pointer; display: flex; }
  .secret-val button:hover { color: var(--accent); }
  .code-input {
    background: var(--input-bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.75rem; border-radius: var(--radius);
    font-size: 1.5rem; font-family: monospace; letter-spacing: 0.5rem;
    text-align: center; width: 100%;
    transition: border-color var(--transition), box-shadow var(--transition);
  }
  .code-input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
  .btn-primary {
    display: flex; align-items: center; justify-content: center; gap: 0.4rem;
    background: var(--accent); border: none; color: #fff;
    padding: 0.65rem; border-radius: var(--radius);
    font-size: 0.9rem; font-weight: 600; cursor: pointer;
    transition: background var(--transition);
  }
  .btn-primary:hover:not(:disabled) { background: var(--accent-h); }
  .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-danger {
    display: flex; align-items: center; gap: 0.4rem;
    background: var(--danger-bg); border: 1px solid var(--danger);
    color: var(--danger); padding: 0.6rem 1rem;
    border-radius: var(--radius); font-size: 0.875rem; cursor: pointer;
    transition: background var(--transition);
  }
  .btn-danger:hover:not(:disabled) { background: var(--danger); color: #fff; }
  .btn-danger:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-secondary {
    background: var(--surface2); border: 1px solid var(--border);
    color: var(--text2); padding: 0.5rem 0.9rem;
    border-radius: var(--radius); cursor: pointer; font-size: 0.85rem;
    transition: background var(--transition);
  }
  .btn-secondary:hover { background: var(--row-hover); color: var(--text); }
  .action-row { display: flex; gap: 0.5rem; }
  input[type="password"] {
    background: var(--input-bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.6rem 0.75rem;
    border-radius: var(--radius); font-size: 0.9rem; width: 100%;
  }
  input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
  /* Done state */
  .done-icon {
    width: 52px; height: 52px; border-radius: 50%;
    background: var(--success-bg); color: var(--success);
    display: grid; place-items: center; margin: 0 auto;
    border: 2px solid var(--success);
    animation: pop-in 0.3s cubic-bezier(.2,.8,.3,1) both;
  }
  @keyframes pop-in {
    from { transform: scale(0.4); opacity: 0; }
    to   { transform: none; opacity: 1; }
  }
  .done-title { text-align: center; font-size: 1rem; font-weight: 700; color: var(--text); }
  .backup-section { display: flex; flex-direction: column; gap: 0.6rem; }
  .backup-warn {
    display: flex; align-items: flex-start; gap: 0.4rem;
    background: rgba(245,166,35,0.1); border: 1px solid var(--warning);
    color: var(--warning); padding: 0.65rem 0.75rem;
    border-radius: var(--radius); font-size: 0.8rem; line-height: 1.4;
  }
  .backup-grid {
    display: grid; grid-template-columns: 1fr 1fr;
    gap: 0.4rem;
  }
  .backup-code {
    background: var(--surface2); border: 1px solid var(--border);
    padding: 0.45rem 0.65rem; border-radius: var(--radius);
    font-family: monospace; font-size: 0.82rem; color: var(--text);
    text-align: center;
  }
</style>
