<script>
  import { onMount } from 'svelte'
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import { locale } from '../stores/i18n.js'
  import Icon from './Icon.svelte'
  import TwoFA from './TwoFA.svelte'

  export let onClose = () => {}

  let activeSection = 'storage'
  let showTwoFA = false
  let settings = null
  let oldPassword = '', newPassword = '', newPassword2 = ''
  let saving = false, settingsLoading = true, settingsError = false, settingsErrorMsg = ''

  // API Tokens
  let tokens = []
  let tokensLoading = false
  let newTokenName = ''
  let newTokenExpiry = 0   // 0 = never, else days
  let createdToken = null  // einmalig anzeigen

  onMount(async () => {
    await loadSettings()
    await loadTokens()
  })

  async function loadSettings() {
    settingsLoading = true; settingsError = false
    try {
      const res = await apiFetch('/api/me/settings')
      if (res.ok) settings = await res.json()
      else { settingsError = true; settingsErrorMsg = `Server returned ${res.status}` }
    } catch { settingsError = true; settingsErrorMsg = 'Server unreachable' }
    settingsLoading = false
  }

  async function loadTokens() {
    tokensLoading = true
    const res = await apiFetch('/api/me/tokens')
    if (res.ok) tokens = await res.json() ?? []
    tokensLoading = false
  }

  async function createToken() {
    if (!newTokenName.trim()) { error('Token name required'); return }
    const res = await apiFetch('/api/me/tokens', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: newTokenName, expires_in_days: newTokenExpiry > 0 ? newTokenExpiry : null })
    })
    if (!res.ok) { error('Could not create token'); return }
    const tok = await res.json()
    createdToken = tok.token
    newTokenName = ''
    newTokenExpiry = 0
    success('Token created — copy it now, it won\'t be shown again!')
    await loadTokens()
  }

  async function deleteToken(id) {
    if (!confirm('Revoke this token?')) return
    await apiFetch(`/api/me/tokens/${id}`, { method: 'DELETE' })
    success('Token revoked')
    await loadTokens()
  }

  async function changePassword() {
    if (newPassword !== newPassword2) { error('Passwords do not match'); return }
    if (newPassword.length < 8) { error('Min 8 characters'); return }
    saving = true
    const res = await apiFetch('/api/me/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword })
    })
    saving = false
    if (res.ok) { success('Password changed'); oldPassword = newPassword = newPassword2 = '' }
    else error(await res.text())
  }

  function fmt(b) {
    if (!b) return 'No limit'
    const u = ['B','KB','MB','GB','TB']
    const i = Math.floor(Math.log(b) / Math.log(1024))
    return (b / 1024 ** i).toFixed(1) + ' ' + u[i]
  }

  function fmtDate(d) {
    if (!d) return '—'
    return new Date(d).toLocaleDateString(undefined, { day:'2-digit', month:'short', year:'numeric' })
  }

  $: webdavUrl = `${location.origin}/webdav`
  $: usedPct = settings?.quota_bytes > 0
    ? Math.min(100, Math.round(settings.disk_usage / settings.quota_bytes * 100)) : null

  function handleKey(e) { if (e.key === 'Escape') onClose() }
</script>

<svelte:window onkeydown={handleKey} />

<div class="overlay" role="dialog" aria-modal="true" tabindex="-1"
  onclick={(e) => { if (e.target === e.currentTarget) onClose() }}
  onkeydown={handleKey}>
  <div class="modal">

    <!-- Header -->
    <div class="modal-header">
      <div class="modal-title">
        <span class="title-icon"><Icon name="settings" size={16} /></span>
        <div>
          <strong>Settings</strong>
          <small>Manage your workspace</small>
        </div>
      </div>
      <button class="close-btn" onclick={onClose}><Icon name="x" size={14} /></button>
    </div>

    <div class="modal-layout">
      <!-- Nav -->
      <nav class="modal-nav">
        {#each [
          { id:'storage',  icon:'save',      label:'Storage' },
          { id:'tokens',   icon:'lock',      label:'API Tokens' },
          { id:'webdav',   icon:'webdav',    label:'WebDAV' },
          { id:'language', icon:'globe',     label:'Language' },
          { id:'password', icon:'lock',      label:'Security' },
        ] as s}          <button class="nav-item" class:active={activeSection === s.id}
            onclick={() => activeSection = s.id}>
            <Icon name={s.icon} size={14} />
            <span>{s.label}</span>
          </button>
        {/each}
      </nav>

      <!-- Body -->
      <div class="modal-body">

        <!-- Storage -->
        {#if activeSection === 'storage'}
          <div class="section-head"><h3>Storage</h3></div>
          {#if settingsLoading}
            <div class="state-row"><span class="spinner"></span> Loading…</div>
          {:else if settingsError}
            <div class="state-row danger"><Icon name="warning" size={14} /> {settingsErrorMsg}
              <button class="link-btn" onclick={loadSettings}>Retry</button>
            </div>
          {:else if settings}
            <div class="info-grid">
              <span class="info-label">Used</span>
              <span class="info-val">{fmt(settings.disk_usage)}</span>
              <span class="info-label">Quota</span>
              <span class="info-val">{fmt(settings.quota_bytes)}</span>
              {#if settings.allowed_types}
                <span class="info-label">Allowed types</span>
                <span class="info-val mono">{settings.allowed_types}</span>
              {/if}
            </div>
            {#if usedPct !== null}
              <div class="quota-bar">
                <div class="quota-fill" style="width:{usedPct}%"
                  class:warn={usedPct > 80} class:crit={usedPct > 95}></div>
              </div>
              <p class="quota-label">{usedPct}% used</p>
            {/if}
          {/if}

        <!-- API Tokens -->
        {:else if activeSection === 'tokens'}
          <div class="section-head">
            <h3>API Tokens</h3>
            <p class="section-sub">Long-lived tokens for scripts and integrations. Prefix: <code>fsk_</code></p>
          </div>

          {#if createdToken}
            <div class="token-reveal">
              <Icon name="warning" size={14} />
              <span>Copy this token — it won't be shown again.</span>
              <div class="token-val">
                <code>{createdToken}</code>
                <button onclick={() => { navigator.clipboard.writeText(createdToken); success('Copied!') }}>
                  <Icon name="clipboard" size={13} />
                </button>
              </div>
              <button class="link-btn" onclick={() => createdToken = null}>Dismiss</button>
            </div>
          {/if}

          <!-- Create form -->
          <div class="token-create">
            <input placeholder="Token name (e.g. backup-script)" bind:value={newTokenName}
              onkeydown={(e) => e.key === 'Enter' && createToken()} />
            <select bind:value={newTokenExpiry}>
              <option value={0}>Never expires</option>
              <option value={30}>30 days</option>
              <option value={90}>90 days</option>
              <option value={365}>1 year</option>
            </select>
            <button class="btn-primary" onclick={createToken}>
              <Icon name="plus" size={13} /> Create
            </button>
          </div>

          <!-- Token list -->
          {#if tokensLoading}
            <div class="state-row"><span class="spinner"></span> Loading tokens…</div>
          {:else if tokens.length === 0}
            <p class="empty-hint">No tokens yet.</p>
          {:else}
            <div class="token-list">
              {#each tokens as tok (tok.id)}
                <div class="token-item">
                  <div class="token-meta">
                    <span class="token-name">{tok.name}</span>
                    <code class="token-prefix">{tok.prefix}</code>
                  </div>
                  <div class="token-dates">
                    <span>Created {fmtDate(tok.created_at)}</span>
                    {#if tok.expires_at}
                      <span class="text-warn">· Expires {fmtDate(tok.expires_at)}</span>
                    {:else}
                      <span class="text-muted">· Never expires</span>
                    {/if}
                    {#if tok.last_used}
                      <span class="text-muted">· Last used {fmtDate(tok.last_used)}</span>
                    {/if}
                  </div>
                  <button class="revoke-btn" onclick={() => deleteToken(tok.id)} title="Revoke">
                    <Icon name="trash" size={13} />
                  </button>
                </div>
              {/each}
            </div>
          {/if}

        <!-- WebDAV -->
        {:else if activeSection === 'webdav'}
          <div class="section-head"><h3>WebDAV Connection</h3></div>
          <p class="section-sub">Connect Finder, Windows Explorer, Cyberduck or any WebDAV client:</p>
          <div class="url-box">
            <code>{webdavUrl}</code>
            <button onclick={() => { navigator.clipboard.writeText(webdavUrl); success('Copied!') }}>
              <Icon name="clipboard" size={13} />
            </button>
          </div>
          <p class="section-sub">Use your Fileship username and password to authenticate.</p>

        <!-- Language -->
        {:else if activeSection === 'language'}
          <div class="section-head"><h3>Language</h3></div>
          <label class="field-label" for="lang-select">Interface language</label>
          <select id="lang-select" bind:value={$locale}>
            <option value="en">English</option>
            <option value="de">Deutsch</option>
          </select>

        <!-- Password -->
        {:else if activeSection === 'password'}
          <div class="section-head"><h3>Change Password</h3></div>
          <div class="form-stack">
            <input type="password" placeholder="Current password" bind:value={oldPassword} />
            <input type="password" placeholder="New password (min 8 chars)" bind:value={newPassword} />
            <input type="password" placeholder="Confirm new password" bind:value={newPassword2} />
            <button class="btn-primary" onclick={changePassword} disabled={saving}>
              {saving ? 'Saving…' : 'Change Password'}
            </button>
          </div>
          <div class="section-head" style="margin-top:0.5rem"><h3>Two-Factor Authentication</h3></div>
          <p class="section-sub">Add an extra layer of security using a TOTP authenticator app.</p>
          <button class="btn-primary" onclick={() => showTwoFA = true}>
            <Icon name="lock" size={14} /> Manage 2FA
          </button>
        {/if}

      </div>
    </div>
  </div>
</div>

{#if showTwoFA}
  <TwoFA onClose={() => showTwoFA = false} />
{/if}

<style>
  .overlay {
    position: fixed; inset: 0;
    background: rgba(0,0,0,0.65);
    display: flex; align-items: center; justify-content: center;
    z-index: 800; padding: 1rem;
    backdrop-filter: blur(2px);
  }

  .modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    width: min(100%, 780px); max-height: 90vh;
    display: flex; flex-direction: column;
    overflow: hidden; box-shadow: var(--shadow-lg);
    animation: modal-in 0.2s cubic-bezier(.2,.8,.3,1) both;
  }

  @keyframes modal-in {
    from { opacity: 0; transform: scale(0.96) translateY(8px); }
    to   { opacity: 1; transform: none; }
  }

  .modal-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 1rem 1.25rem; border-bottom: 1px solid var(--border);
  }

  .modal-title {
    display: flex; align-items: center; gap: 0.65rem;
  }

  .title-icon {
    width: 32px; height: 32px; border-radius: var(--radius);
    background: var(--accent-soft); color: var(--accent);
    display: grid; place-items: center; flex-shrink: 0;
  }

  .modal-title strong { font-size: 0.95rem; display: block; }
  .modal-title small { font-size: 0.73rem; color: var(--text2); }

  .close-btn {
    background: none; border: none; color: var(--text2);
    cursor: pointer; padding: 0.35rem; border-radius: var(--radius);
    display: flex; transition: background var(--transition);
  }
  .close-btn:hover { background: var(--row-hover); color: var(--text); }

  .modal-layout {
    display: grid; grid-template-columns: 168px 1fr;
    min-height: 0; overflow: hidden;
  }

  .modal-nav {
    padding: 0.75rem 0.6rem;
    border-right: 1px solid var(--border);
    background: var(--surface2);
    display: flex; flex-direction: column; gap: 2px;
    overflow-y: auto;
  }

  .nav-item {
    display: flex; align-items: center; gap: 0.55rem;
    padding: 0.6rem 0.7rem; border-radius: var(--radius);
    color: var(--text2); font-size: 0.83rem;
    background: none; border: none; cursor: pointer;
    text-align: left; width: 100%;
    transition: background var(--transition), color var(--transition);
  }
  .nav-item:hover { background: var(--row-hover); color: var(--text); }
  .nav-item.active { background: var(--accent-soft); color: var(--accent); font-weight: 600; }

  .modal-body {
    overflow-y: auto; padding: 1.25rem;
    display: flex; flex-direction: column; gap: 0.85rem;
  }

  .section-head { display: flex; flex-direction: column; gap: 0.2rem; border-bottom: 1px solid var(--border2); padding-bottom: 0.75rem; }
  .section-head h3 { font-size: 0.95rem; font-weight: 700; color: var(--text); }
  .section-sub { font-size: 0.8rem; color: var(--text2); line-height: 1.4; }

  /* Storage */
  .info-grid { display: grid; grid-template-columns: 1fr 2fr; gap: 0.35rem 1rem; font-size: 0.875rem; }
  .info-label { color: var(--text2); }
  .info-val { color: var(--text); }
  .mono { font-family: monospace; font-size: 0.8rem; }
  .quota-bar { height: 6px; background: var(--border); border-radius: 3px; overflow: hidden; margin-top: 0.25rem; }
  .quota-fill { height: 100%; background: var(--accent); border-radius: 3px; transition: width 0.4s; }
  .quota-fill.warn { background: var(--warning); }
  .quota-fill.crit { background: var(--danger); }
  .quota-label { font-size: 0.75rem; color: var(--text3); }

  /* API Tokens */
  .token-reveal {
    background: rgba(245,166,35,0.1);
    border: 1px solid var(--warning);
    border-radius: var(--radius); padding: 0.75rem;
    display: flex; flex-direction: column; gap: 0.4rem;
    font-size: 0.82rem; color: var(--warning);
  }
  .token-val {
    display: flex; align-items: center; gap: 0.4rem;
    background: var(--input-bg); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 0.4rem 0.6rem;
  }
  .token-val code { flex: 1; font-size: 0.8rem; color: var(--text); font-family: monospace; word-break: break-all; }
  .token-val button { background: none; border: none; color: var(--text2); cursor: pointer; display: flex; }
  .token-val button:hover { color: var(--accent); }

  .token-create {
    display: flex; gap: 0.5rem; flex-wrap: wrap;
  }
  .token-create input {
    flex: 1; min-width: 160px;
    background: var(--input-bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.5rem 0.7rem;
    border-radius: var(--radius); font-size: 0.875rem;
  }
  .token-create input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
  .token-create select {
    background: var(--input-bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.5rem 0.7rem;
    border-radius: var(--radius); font-size: 0.875rem;
  }

  .token-list { display: flex; flex-direction: column; gap: 0.4rem; }
  .token-item {
    display: flex; align-items: center; gap: 0.75rem;
    background: var(--surface2); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 0.65rem 0.85rem;
  }
  .token-meta { flex: 1; min-width: 0; display: flex; align-items: center; gap: 0.5rem; }
  .token-name { font-size: 0.875rem; font-weight: 600; color: var(--text); }
  .token-prefix { font-size: 0.75rem; color: var(--text3); font-family: monospace; background: var(--bg); padding: 0.1rem 0.35rem; border-radius: 4px; }
  .token-dates { font-size: 0.75rem; color: var(--text3); white-space: nowrap; display: flex; gap: 0.3rem; flex-wrap: wrap; }
  .text-warn { color: var(--warning); }
  .text-muted { color: var(--text3); }
  .revoke-btn { background: none; border: none; color: var(--text3); cursor: pointer; display: flex; padding: 0.25rem; border-radius: 4px; }
  .revoke-btn:hover { background: var(--danger-bg); color: var(--danger); }

  .empty-hint { font-size: 0.82rem; color: var(--text3); }

  /* WebDAV */
  .url-box {
    display: flex; align-items: center; gap: 0.5rem;
    background: var(--input-bg); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 0.6rem 0.8rem;
  }
  .url-box code { flex: 1; font-size: 0.82rem; color: var(--accent); font-family: monospace; word-break: break-all; }
  .url-box button { background: none; border: none; color: var(--text2); cursor: pointer; display: flex; }
  .url-box button:hover { color: var(--accent); }

  /* Language */
  select {
    background: var(--input-bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.5rem 0.75rem;
    border-radius: var(--radius); font-size: 0.875rem;
  }
  select:focus { outline: none; border-color: var(--accent); }

  /* Password / generic form */
  .form-stack { display: flex; flex-direction: column; gap: 0.6rem; }

  input[type="password"],
  input:not([type]) {
    background: var(--input-bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.55rem 0.75rem;
    border-radius: var(--radius); font-size: 0.875rem; width: 100%;
    transition: border-color var(--transition), box-shadow var(--transition);
  }
  input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }

  .btn-primary {
    background: var(--accent); border: none; color: #fff;
    padding: 0.55rem 1rem; border-radius: var(--radius);
    font-size: 0.875rem; font-weight: 600; cursor: pointer;
    display: inline-flex; align-items: center; gap: 0.35rem;
    transition: background var(--transition);
    white-space: nowrap;
  }
  .btn-primary:hover:not(:disabled) { background: var(--accent-h); }
  .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }

  .field-label { font-size: 0.75rem; font-weight: 700; color: var(--text3); text-transform: uppercase; letter-spacing: 0.06em; display: block; margin-bottom: 0.3rem; }

  /* States */
  .state-row {
    display: flex; align-items: center; gap: 0.5rem;
    font-size: 0.82rem; color: var(--text2); padding: 0.5rem 0;
  }
  .state-row.danger { color: var(--danger); }
  .link-btn { background: none; border: none; color: var(--accent); cursor: pointer; font-size: 0.82rem; padding: 0; }
  .link-btn:hover { text-decoration: underline; }

  .spinner {
    width: 14px; height: 14px;
    border: 2px solid var(--border); border-top-color: var(--accent);
    border-radius: 50%; animation: spin 0.7s linear infinite; flex-shrink: 0;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  @media (max-width: 600px) {
    .modal-layout { grid-template-columns: 1fr; }
    .modal-nav { flex-direction: row; overflow-x: auto; border-right: none; border-bottom: 1px solid var(--border); padding: 0.4rem; }
    .nav-item { white-space: nowrap; }
    .modal-body { padding: 0.9rem; }
    .token-create { flex-direction: column; }
    .token-dates { display: none; }
  }
</style>
