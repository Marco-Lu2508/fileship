<script>
  import { onMount } from 'svelte'
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import { locale } from '../stores/i18n.js'
  import Icon from './Icon.svelte'

  export let onClose = () => {}

  let settings = null
  let oldPassword = ''
  let newPassword = ''
  let newPassword2 = ''
  let saving = false
  let settingsLoading = true
  let settingsError = false

  onMount(loadSettings)

  async function loadSettings() {
    settingsLoading = true
    settingsError = false
    const res = await apiFetch('/api/me/settings')
    if (res.ok) settings = await res.json()
    else settingsError = true
    settingsLoading = false
  }

  async function changePassword() {
    if (newPassword !== newPassword2) { error('Passwords do not match'); return }
    if (newPassword.length < 8) { error('Password too short (min 8 chars)'); return }
    saving = true
    const res = await apiFetch('/api/me/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword })
    })
    saving = false
    if (res.ok) {
      success('Password changed')
      oldPassword = newPassword = newPassword2 = ''
    } else {
      error(await res.text())
    }
  }

  function formatBytes(b) {
    if (!b) return 'No limit'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(b) / Math.log(1024))
    return (b / Math.pow(1024, i)).toFixed(1) + ' ' + units[i]
  }

  $: webdavUrl = `${location.origin}/webdav`
  $: usedPct = settings?.quota_bytes > 0
    ? Math.min(100, Math.round((settings.disk_usage / settings.quota_bytes) * 100))
    : null

  function handleKey(e) { if (e.key === 'Escape') onClose() }
</script>

<svelte:window onkeydown={handleKey} />

<div class="overlay" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => { if (e.target === e.currentTarget) onClose() }} onkeydown={handleKey}>
  <div class="settings-modal">
    <div class="settings-header">
      <div class="settings-title"><span class="settings-mark"><Icon name="settings" size={17} /></span><div><strong>Settings</strong><small>Manage your Fileship workspace</small></div></div>
      <button class="close" onclick={onClose} title="Close"><Icon name="x" size={15} /></button>
    </div>

    <div class="settings-layout">
      <nav class="settings-nav" aria-label="Settings sections">
        <a href="#storage" class="active"><Icon name="save" size={15} /> Storage</a>
        <a href="#webdav"><Icon name="webdav" size={15} /> Connections</a>
        <a href="#language"><Icon name="globe" size={15} /> Preferences</a>
        <a href="#password"><Icon name="lock" size={15} /> Security</a>
      </nav>

      <div class="settings-body">

      <!-- Storage -->
      <section id="storage">
        <h3><Icon name="save" size={15} /> Storage</h3>
        {#if settingsLoading}
          <div class="loading-row"><span class="spinner"></span><p class="muted">Loading storage details...</p></div>
        {:else if settingsError}
          <div class="error-row"><Icon name="warning" size={15} /><span>Could not load storage details.</span><button class="retry" onclick={loadSettings}>Retry</button></div>
        {:else if settings}
          <div class="info-row">
            <span>Used</span>
            <span>{formatBytes(settings.disk_usage)}</span>
          </div>
          <div class="info-row">
            <span>Quota</span>
            <span>{formatBytes(settings.quota_bytes)}</span>
          </div>
          {#if usedPct !== null}
            <div class="quota-bar">
              <div class="quota-fill" style="width:{usedPct}%" class:warn={usedPct > 80} class:crit={usedPct > 95}></div>
            </div>
            <p class="quota-pct">{usedPct}% used</p>
          {/if}
          {#if settings.allowed_types}
            <div class="info-row">
              <span>Allowed types</span>
              <span class="mono">{settings.allowed_types}</span>
            </div>
          {/if}
        {/if}
      </section>

      <!-- WebDAV -->
      <section id="webdav">
        <h3><Icon name="webdav" size={15} /> WebDAV</h3>
        <p class="muted">Connect with Finder, Windows Explorer, Cyberduck or any WebDAV client:</p>
        <div class="webdav-url">
          <code>{webdavUrl}</code>
          <button onclick={() => { navigator.clipboard.writeText(webdavUrl); success('Copied!') }} title="Copy URL"><Icon name="clipboard" size={14} /></button>
        </div>
        <p class="muted small">Use your Fileship username and password to authenticate.</p>
      </section>

      <!-- Language -->
      <section id="language">
        <h3><Icon name="globe" size={15} /> Language</h3>
        <select bind:value={$locale}>
          <option value="en">English</option>
          <option value="de">Deutsch</option>
        </select>
      </section>

      <!-- Password -->
      <section id="password">
        <h3><Icon name="lock" size={15} /> Change Password</h3>
        <div class="form">
          <input type="password" placeholder="Current password" bind:value={oldPassword} />
          <input type="password" placeholder="New password (min 8 chars)" bind:value={newPassword} />
          <input type="password" placeholder="Confirm new password" bind:value={newPassword2} />
          <button onclick={changePassword} disabled={saving}>
            {saving ? 'Saving…' : 'Change Password'}
          </button>
        </div>
      </section>

      </div>
    </div>
  </div>
</div>

<style>
  .overlay { position: fixed; inset: 0; background: rgba(8, 15, 25, 0.72); display: flex; align-items: center; justify-content: center; z-index: 800; padding: 1rem; }
  .settings-modal { background: var(--surface); border: 1px solid var(--border); border-radius: 14px; width: min(100%, 820px); max-height: 90vh; display: flex; flex-direction: column; overflow: hidden; box-shadow: var(--shadow); }
  .settings-header { display: flex; align-items: center; justify-content: space-between; padding: 1.15rem 1.35rem; border-bottom: 1px solid var(--border); color: var(--text); }
  .settings-title { display: flex; align-items: center; gap: 0.7rem; }
  .settings-title > div { display: flex; flex-direction: column; gap: 0.15rem; }
  .settings-title strong { font-size: 1rem; }
  .settings-title small { color: var(--text2); font-size: 0.75rem; font-weight: 400; }
  .settings-mark { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 8px; color: var(--accent); background: var(--row-hover); }
  h3 { display: inline-flex; align-items: center; gap: 0.45rem; }
  .close { display: flex; background: none; border: none; color: var(--text2); cursor: pointer; padding: 0.35rem; border-radius: 5px; }
  .close:hover { color: var(--text); background: var(--row-hover); }
  .settings-layout { display: grid; grid-template-columns: 180px 1fr; min-height: 0; overflow: hidden; }
  .settings-nav { padding: 1rem 0.7rem; border-right: 1px solid var(--border); background: var(--surface2); }
  .settings-nav a { display: flex; align-items: center; gap: 0.55rem; padding: 0.65rem 0.7rem; margin-bottom: 0.2rem; border-radius: 7px; color: var(--text2); font-size: 0.82rem; text-decoration: none; }
  .settings-nav a:hover, .settings-nav a.active { background: var(--row-hover); color: var(--accent); }
  .settings-body { overflow-y: auto; padding: 1.25rem; display: flex; flex-direction: column; gap: 1rem; }
  section { scroll-margin-top: 1rem; display: flex; flex-direction: column; gap: 0.6rem; padding: 1.1rem; border: 1px solid var(--border); border-radius: 10px; background: var(--surface2); }
  h3 { font-size: 0.9rem; font-weight: 600; color: var(--text); margin: 0; }
  .info-row { display: flex; justify-content: space-between; font-size: 0.9rem; color: var(--text2); }
  .info-row span:last-child { color: var(--text); }
  .quota-bar { height: 6px; background: var(--border); border-radius: 3px; overflow: hidden; }
  .quota-fill { height: 100%; background: var(--accent); border-radius: 3px; transition: width 0.3s; }
  .quota-fill.warn { background: #f59e0b; }
  .quota-fill.crit { background: var(--danger); }
  .quota-pct { font-size: 0.8rem; color: var(--text2); }
  .webdav-url { display: flex; align-items: center; gap: 0.5rem; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 0.5rem 0.75rem; }
  code { flex: 1; font-size: 0.85rem; color: var(--accent); font-family: monospace; word-break: break-all; }
  .webdav-url button { display: flex; background: none; border: none; color: var(--text2); cursor: pointer; flex-shrink: 0; }
  select { background: var(--bg); border: 1px solid var(--border); color: var(--text); padding: 0.5rem 0.75rem; border-radius: 6px; font-size: 0.9rem; }
  .form { display: flex; flex-direction: column; gap: 0.6rem; }
  input { background: var(--bg); border: 1px solid var(--border); color: var(--text); padding: 0.5rem 0.75rem; border-radius: 6px; font-size: 0.9rem; }
  input:focus { outline: none; border-color: var(--accent); }
  button { background: var(--accent); border: none; color: #fff; padding: 0.5rem 1rem; border-radius: 6px; cursor: pointer; font-size: 0.9rem; }
  button:hover:not(:disabled) { background: var(--accent-h); }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  .muted { font-size: 0.85rem; color: var(--text2); }
  .small { font-size: 0.8rem; }
  .mono { font-family: monospace; font-size: 0.85rem; }
  .loading-row { display: flex; align-items: center; gap: 0.6rem; }
  .error-row { display: flex; align-items: center; flex-wrap: wrap; gap: 0.5rem; color: var(--danger); font-size: 0.85rem; }
  .retry { margin-left: auto; padding: 0.35rem 0.6rem; background: var(--surface); color: var(--accent); border: 1px solid var(--border); }
  .spinner { width: 16px; height: 16px; border: 2px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (max-width: 620px) { .settings-layout { display: block; overflow: auto; } .settings-nav { display: flex; gap: 0.25rem; overflow-x: auto; border-right: 0; border-bottom: 1px solid var(--border); padding: 0.6rem; } .settings-nav a { white-space: nowrap; margin: 0; } .settings-body { overflow: visible; padding: 0.8rem; } .settings-title small { display: none; } }
</style>
