<script>
  import Modal from './Modal.svelte'
  import { locale, translator as i18n } from '../stores/i18n.js'
  import { onMount } from 'svelte'
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import { languages } from '../stores/i18n.js'
  import Icon from './Icon.svelte'
  import TwoFA from './TwoFA.svelte'
  import { preferences } from '../stores/preferences.js'
  import { formatBytes } from '../lib/files.js'
  import {
    sources,
    agents,
    activeSourceId,
    activeAgentId,
    loadSources,
    loadAgents,
    selectLocalWorkspace,
    selectRemoteAgent
  } from '../stores/sources.js'
  import { loadFiles } from '../stores/files.js'

  export let onClose = () => {}

  let activeSection = 'storage'
  let showTwoFA = false
  let settings = null
  let oldPassword = '',
    newPassword = '',
    newPassword2 = ''
  let saving = false,
    settingsLoading = true,
    settingsError = false,
    settingsErrorMsg = ''

  // API Tokens
  let tokens = []
  let tokensLoading = false
  let newTokenName = ''
  let newTokenExpiry = 0 // 0 = never, else days
  let createdToken = null // einmalig anzeigen

  // Remote Agents & Sources
  let newAgentName = ''
  let newAgentRoot = './data'
  let createdAgent = null
  let newSourceName = ''
  let newSourceRoot = ''

  onMount(async () => {
    await loadSettings()
    await loadTokens()
    await loadAgents()
    await loadSources()
  })

  async function createAgentDevice() {
    if (!newAgentName.trim()) {
      error('Device name required')
      return
    }
    const res = await apiFetch('/api/me/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: newAgentName.trim(),
        root_path: newAgentRoot.trim() || './data'
      })
    })
    if (!res.ok) {
      error('Could not register device')
      return
    }
    createdAgent = await res.json()
    newAgentName = ''
    success('Device registered — copy the agent start command below!')
    await loadAgents()
  }

  async function removeAgentDevice(id) {
    if (!confirm('Remove this remote device?')) return
    await apiFetch(`/api/me/agents/${id}`, { method: 'DELETE' })
    if ($activeAgentId === id) {
      selectLocalWorkspace(null)
      await loadFiles('')
    }
    await loadAgents()
    success('Device removed')
  }

  async function createLocalSource() {
    if (!newSourceName.trim() || !newSourceRoot.trim()) {
      error('Name and path required')
      return
    }
    const res = await apiFetch('/api/me/sources', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: newSourceName.trim(),
        root_path: newSourceRoot.trim(),
        is_default: false
      })
    })
    if (!res.ok) {
      error('Could not create source')
      return
    }
    newSourceName = ''
    newSourceRoot = ''
    await loadSources()
    success('Source added')
  }

  async function removeLocalSource(id) {
    await apiFetch(`/api/me/sources/${id}`, { method: 'DELETE' })
    if ($activeSourceId === id) {
      selectLocalWorkspace(null)
      await loadFiles('')
    }
    await loadSources()
  }

  async function loadSettings() {
    settingsLoading = true
    settingsError = false
    try {
      const res = await apiFetch('/api/me/settings')
      if (res.ok) settings = await res.json()
      else {
        settingsError = true
        settingsErrorMsg = $i18n('Server returned {status}', {
          status: res.status
        })
      }
    } catch {
      settingsError = true
      settingsErrorMsg = $i18n('Server unreachable')
    }
    settingsLoading = false
  }

  async function loadTokens() {
    tokensLoading = true
    const res = await apiFetch('/api/me/tokens')
    if (res.ok) tokens = (await res.json()) ?? []
    tokensLoading = false
  }

  async function createToken() {
    if (!newTokenName.trim()) {
      error($i18n('Token name required'))
      return
    }
    const res = await apiFetch('/api/me/tokens', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: newTokenName,
        expires_in_days: newTokenExpiry > 0 ? newTokenExpiry : null
      })
    })
    if (!res.ok) {
      error($i18n('Could not create token'))
      return
    }
    const tok = await res.json()
    createdToken = tok.token
    newTokenName = ''
    newTokenExpiry = 0
    success($i18n("Token created — copy it now, it won't be shown again!"))
    await loadTokens()
  }

  async function deleteToken(id) {
    if (!confirm($i18n('Revoke this token?'))) return
    await apiFetch(`/api/me/tokens/${id}`, { method: 'DELETE' })
    success($i18n('Token revoked'))
    await loadTokens()
  }

  async function changePassword() {
    if (newPassword !== newPassword2) {
      error($i18n('Passwords do not match'))
      return
    }
    if (newPassword.length < 8) {
      error($i18n('Min 8 characters'))
      return
    }
    saving = true
    const res = await apiFetch('/api/me/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        old_password: oldPassword,
        new_password: newPassword
      })
    })
    saving = false
    if (res.ok) {
      success($i18n('Password changed'))
      oldPassword = newPassword = newPassword2 = ''
    } else error(await res.text())
  }

  function fmtDate(d) {
    if (!d) return '—'
    return new Date(d).toLocaleDateString($locale, {
      day: '2-digit',
      month: 'short',
      year: 'numeric'
    })
  }

  $: webdavUrl = `${location.origin}/webdav`
  $: usedPct =
    settings?.quota_bytes > 0
      ? Math.min(
          100,
          Math.round((settings.disk_usage / settings.quota_bytes) * 100)
        )
      : null
</script>

<Modal label={$i18n('Settings')} width="780px" {onClose}>
  <div class="modal">
    <!-- Header -->
    <div class="modal-header">
      <div class="modal-title">
        <span class="title-icon"><Icon name="settings" size={16} /></span>
        <div>
          <strong>{$i18n('Settings')}</strong>
          <small>{$i18n('Manage your workspace')}</small>
        </div>
      </div>
      <button class="close-btn" onclick={onClose} aria-label={$i18n('Close')}
        ><Icon name="x" size={14} /></button
      >
    </div>

    <div class="modal-layout">
      <!-- Nav -->
      <nav class="modal-nav">
        {#each [{ id: 'storage', icon: 'save', label: $i18n('Storage') }, { id: 'devices', icon: 'webdav', label: 'Devices / Agents' }, { id: 'tokens', icon: 'lock', label: $i18n('API Tokens') }, { id: 'webdav', icon: 'webdav', label: 'WebDAV' }, { id: 'language', icon: 'globe', label: $i18n('Language') }, { id: 'browser', icon: 'folder', label: $i18n('File browser') }, { id: 'password', icon: 'lock', label: $i18n('Security') }] as s}
          <button
            class="nav-item"
            class:active={activeSection === s.id}
            onclick={() => (activeSection = s.id)}
          >
            <Icon name={s.icon} size={14} />
            <span>{s.label}</span>
          </button>
        {/each}
      </nav>

      <!-- Body -->
      <div class="modal-body">
        <!-- Storage -->
        {#if activeSection === 'storage'}
          <div class="section-head"><h3>{$i18n('Storage')}</h3></div>
          {#if settingsLoading}
            <div class="state-row">
              <span class="spinner"></span>
              {$i18n('Loading…')}
            </div>
          {:else if settingsError}
            <div class="state-row danger">
              <Icon name="warning" size={14} />
              {settingsErrorMsg}
              <button class="link-btn" onclick={loadSettings}
                >{$i18n('Retry')}</button
              >
            </div>
          {:else if settings}
            <div class="info-grid">
              <span class="info-label">{$i18n('Used')}</span>
              <span class="info-val"
                >{formatBytes(settings.disk_usage, $locale)}</span
              >
              <span class="info-label">{$i18n('Quota')}</span>
              <span class="info-val"
                >{settings.quota_bytes > 0
                  ? formatBytes(settings.quota_bytes, $locale)
                  : $i18n('No limit')}</span
              >
              {#if settings.allowed_types}
                <span class="info-label">{$i18n('Allowed types')}</span>
                <span class="info-val mono">{settings.allowed_types}</span>
              {/if}
            </div>
            {#if usedPct !== null}
              <div class="quota-bar">
                <div
                  class="quota-fill"
                  style="width:{usedPct}%"
                  class:warn={usedPct > 80}
                  class:crit={usedPct > 95}
                ></div>
              </div>
              <p class="quota-label">{usedPct}{$i18n('% used')}</p>
            {/if}
          {/if}

          <!-- Devices & Remote Agents -->
        {:else if activeSection === 'devices'}
          <div class="section-head">
            <h3>Remote Devices (Agents)</h3>
            <p class="section-sub">
              Install the Fileship Agent on your other PCs, NAS or servers to
              browse, stream and manage their files directly from here.
            </p>
          </div>

          {#if createdAgent}
            <div class="token-reveal">
              <Icon name="check" size={14} />
              <span
                >Run this command on <strong>{createdAgent.name}</strong> to connect:</span
              >
              <div class="token-val">
                <code
                  >fileship --agent --server {location.origin} --token {createdAgent.token}
                  --root "{createdAgent.root_path || './data'}"</code
                >
                <button
                  onclick={() => {
                    navigator.clipboard.writeText(
                      `fileship --agent --server ${location.origin} --token ${createdAgent.token} --root "${createdAgent.root_path || './data'}"`
                    )
                    success($i18n('Copied!'))
                  }}
                >
                  <Icon name="clipboard" size={13} />
                </button>
              </div>
              <button class="link-btn" onclick={() => (createdAgent = null)}
                >{$i18n('Dismiss')}</button
              >
            </div>
          {/if}

          <div class="token-create">
            <input
              placeholder="Device name (e.g. Home-NAS, Gaming-PC)"
              bind:value={newAgentName}
            />
            <input
              placeholder="Remote folder path (e.g. ./data or C:\Shares)"
              bind:value={newAgentRoot}
            />
            <button class="btn-primary" onclick={createAgentDevice}>
              <Icon name="plus" size={13} /> Add Device
            </button>
          </div>

          {#if $agents.length === 0}
            <p class="empty-hint">No remote devices registered yet.</p>
          {:else}
            <div class="token-list">
              {#each $agents as dev (dev.id)}
                <div class="token-item">
                  <div class="token-meta">
                    <span class="token-name">{dev.name}</span>
                    <code class="token-prefix"
                      >{dev.online ? '🟢 Online' : '⚪ Offline'}</code
                    >
                    {#if dev.os_info}
                      <span class="text-muted" style="font-size:0.75rem"
                        >{dev.hostname ? `${dev.hostname} · ` : ''}{dev.os_info} ·
                        {formatBytes(dev.disk_usage, $locale)}</span
                      >
                    {/if}
                  </div>
                  <div class="token-dates">
                    <button
                      class="btn-primary"
                      disabled={!dev.online}
                      onclick={async () => {
                        selectRemoteAgent(dev.id)
                        await loadFiles('')
                        onClose()
                      }}
                    >
                      {$activeAgentId === dev.id ? 'Active' : 'Open Device'}
                    </button>
                  </div>
                  <button
                    class="revoke-btn"
                    onclick={() => removeAgentDevice(dev.id)}
                    title="Remove device"
                  >
                    <Icon name="trash" size={13} />
                  </button>
                </div>
              {/each}
            </div>
          {/if}

          <div class="section-head" style="margin-top:1.25rem">
            <h3>Local Sources</h3>
            <p class="section-sub">
              Additional folders on this server you can switch between.
            </p>
          </div>
          <div class="token-create">
            <input placeholder="Source name" bind:value={newSourceName} />
            <input placeholder="Folder path" bind:value={newSourceRoot} />
            <button class="btn-primary" onclick={createLocalSource}>
              <Icon name="plus" size={13} /> Add Source
            </button>
          </div>
          {#if $sources.length > 0}
            <div class="token-list">
              {#each $sources as src (src.id)}
                <div class="token-item">
                  <div class="token-meta">
                    <span class="token-name">{src.name}</span>
                    <code class="token-prefix">{src.root_path}</code>
                  </div>
                  <button
                    class="revoke-btn"
                    onclick={() => removeLocalSource(src.id)}
                  >
                    <Icon name="trash" size={13} />
                  </button>
                </div>
              {/each}
            </div>
          {/if}

          <!-- API Tokens -->
        {:else if activeSection === 'tokens'}
          <div class="section-head">
            <h3>{$i18n('API Tokens')}</h3>
            <p class="section-sub">
              {$i18n('Long-lived tokens for scripts and integrations. Prefix:')}
              <code>fsk_</code>
            </p>
          </div>

          {#if createdToken}
            <div class="token-reveal">
              <Icon name="warning" size={14} />
              <span>{$i18n("Copy this token — it won't be shown again.")}</span>
              <div class="token-val">
                <code>{createdToken}</code>
                <button
                  onclick={() => {
                    navigator.clipboard.writeText(createdToken)
                    success($i18n('Copied!'))
                  }}
                >
                  <Icon name="clipboard" size={13} />
                </button>
              </div>
              <button class="link-btn" onclick={() => (createdToken = null)}
                >{$i18n('Dismiss')}</button
              >
            </div>
          {/if}

          <!-- Create form -->
          <div class="token-create">
            <input
              placeholder={$i18n('Token name (e.g. backup-script)')}
              bind:value={newTokenName}
              onkeydown={(e) => e.key === 'Enter' && createToken()}
            />
            <select bind:value={newTokenExpiry}>
              <option value={0}>{$i18n('Never expires')}</option>
              <option value={30}>{$i18n('30 days')}</option>
              <option value={90}>{$i18n('90 days')}</option>
              <option value={365}>{$i18n('1 year')}</option>
            </select>
            <button class="btn-primary" onclick={createToken}>
              <Icon name="plus" size={13} />
              {$i18n('Create')}
            </button>
          </div>

          <!-- Token list -->
          {#if tokensLoading}
            <div class="state-row">
              <span class="spinner"></span>
              {$i18n('Loading tokens…')}
            </div>
          {:else if tokens.length === 0}
            <p class="empty-hint">{$i18n('No tokens yet.')}</p>
          {:else}
            <div class="token-list">
              {#each tokens as tok (tok.id)}
                <div class="token-item">
                  <div class="token-meta">
                    <span class="token-name">{tok.name}</span>
                    <code class="token-prefix">{tok.prefix}</code>
                  </div>
                  <div class="token-dates">
                    <span>{$i18n('Created')} {fmtDate(tok.created_at)}</span>
                    {#if tok.expires_at}
                      <span class="text-warn"
                        >{$i18n('· Expires')} {fmtDate(tok.expires_at)}</span
                      >
                    {:else}
                      <span class="text-muted">{$i18n('· Never expires')}</span>
                    {/if}
                    {#if tok.last_used}
                      <span class="text-muted"
                        >{$i18n('· Last used')} {fmtDate(tok.last_used)}</span
                      >
                    {/if}
                  </div>
                  <button
                    class="revoke-btn"
                    onclick={() => deleteToken(tok.id)}
                    title={$i18n('Revoke')}
                  >
                    <Icon name="trash" size={13} />
                  </button>
                </div>
              {/each}
            </div>
          {/if}
        {:else if activeSection === 'browser'}
          <div class="section-head"><h3>{$i18n('File browser')}</h3></div>
          <label class="preference"
            ><input
              type="checkbox"
              bind:checked={$preferences.hideDotfiles}
            />{$i18n('Hide dotfiles')}</label
          >
          <label class="preference"
            ><input
              type="checkbox"
              bind:checked={$preferences.singleClick}
            />{$i18n('Open files with a single click')}</label
          >
          <label class="preference"
            ><input
              type="checkbox"
              bind:checked={$preferences.exactDates}
            />{$i18n('Show exact dates and times')}</label
          >
          <p class="section-sub">
            {$i18n('Preferences are saved on this device.')}
          </p>
          <!-- WebDAV -->
        {:else if activeSection === 'webdav'}
          <div class="section-head"><h3>{$i18n('WebDAV Connection')}</h3></div>
          <p class="section-sub">
            {$i18n(
              'Connect Finder, Windows Explorer, Cyberduck or any WebDAV client:'
            )}
          </p>
          <div class="url-box">
            <code>{webdavUrl}</code>
            <button
              onclick={() => {
                navigator.clipboard.writeText(webdavUrl)
                success($i18n('Copied!'))
              }}
            >
              <Icon name="clipboard" size={13} />
            </button>
          </div>
          <p class="section-sub">
            {$i18n('Use your Fileship username and password to authenticate.')}
          </p>

          <!-- Language -->
        {:else if activeSection === 'language'}
          <div class="section-head"><h3>{$i18n('Language')}</h3></div>
          <label class="field-label" for="lang-select"
            >{$i18n('Interface language')}</label
          >
          <select id="lang-select" bind:value={$locale}>
            {#each languages as language}<option value={language.id}
                >{language.name}</option
              >{/each}
          </select>

          <!-- Password -->
        {:else if activeSection === 'password'}
          <div class="section-head"><h3>{$i18n('Change Password')}</h3></div>
          <div class="form-stack">
            <input
              type="password"
              placeholder={$i18n('Current password')}
              aria-label={$i18n('Current password')}
              autocomplete="current-password"
              name="current-password"
              bind:value={oldPassword}
            />
            <input
              type="password"
              placeholder={$i18n('New password (min 8 chars)')}
              aria-label={$i18n('New password (min 8 chars)')}
              autocomplete="new-password"
              name="new-password"
              bind:value={newPassword}
            />
            <input
              type="password"
              placeholder={$i18n('Confirm new password')}
              aria-label={$i18n('Confirm new password')}
              autocomplete="new-password"
              name="confirm-password"
              bind:value={newPassword2}
            />
            <button
              class="btn-primary"
              onclick={changePassword}
              disabled={saving}
            >
              {saving ? $i18n('Saving…') : $i18n('Change Password')}
            </button>
          </div>
          <div class="section-head" style="margin-top:0.5rem">
            <h3>{$i18n('Two-Factor Authentication')}</h3>
          </div>
          <p class="section-sub">
            {$i18n(
              'Add an extra layer of security using a TOTP authenticator app.'
            )}
          </p>
          <button class="btn-primary" onclick={() => (showTwoFA = true)}>
            <Icon name="lock" size={14} />
            {$i18n('Manage 2FA')}
          </button>
        {/if}
      </div>
    </div>
  </div>
</Modal>

{#if showTwoFA}
  <TwoFA onClose={() => (showTwoFA = false)} />
{/if}

<style>
  .preference {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 18px 0;
    font-size: 13px;
    color: var(--text);
  }

  .modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    width: min(100%, 780px);
    max-height: calc(100dvh - 32px);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    box-shadow: var(--shadow-lg);
    animation: modal-in 0.2s cubic-bezier(0.2, 0.8, 0.3, 1) both;
  }

  @keyframes modal-in {
    from {
      opacity: 0;
      transform: scale(0.96) translateY(8px);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 1rem 1.25rem;
    border-bottom: 1px solid var(--border);
  }

  .modal-title {
    display: flex;
    align-items: center;
    gap: 0.65rem;
  }

  .title-icon {
    width: 32px;
    height: 32px;
    border-radius: var(--radius);
    background: var(--accent-soft);
    color: var(--accent);
    display: grid;
    place-items: center;
    flex-shrink: 0;
  }

  .modal-title strong {
    font-size: 0.95rem;
    display: block;
  }
  .modal-title small {
    font-size: 0.73rem;
    color: var(--text2);
  }

  .close-btn {
    background: none;
    border: none;
    color: var(--text2);
    cursor: pointer;
    padding: 0.35rem;
    border-radius: var(--radius);
    display: flex;
    transition: background var(--transition);
  }
  .close-btn:hover {
    background: var(--row-hover);
    color: var(--text);
  }

  .modal-layout {
    display: grid;
    grid-template-columns: 168px 1fr;
    min-height: 0;
    overflow: hidden;
  }

  .modal-nav {
    padding: 0.75rem 0.6rem;
    border-right: 1px solid var(--border);
    background: var(--surface2);
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow-y: auto;
  }

  .nav-item {
    display: flex;
    align-items: center;
    gap: 0.55rem;
    padding: 0.6rem 0.7rem;
    border-radius: var(--radius);
    color: var(--text2);
    font-size: 0.83rem;
    background: none;
    border: none;
    cursor: pointer;
    text-align: left;
    width: 100%;
    transition:
      background var(--transition),
      color var(--transition);
  }
  .nav-item:hover {
    background: var(--row-hover);
    color: var(--text);
  }
  .nav-item.active {
    background: var(--accent-soft);
    color: var(--accent);
    font-weight: 600;
  }

  .modal-body {
    overflow-y: auto;
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 0.85rem;
  }

  .section-head {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    border-bottom: 1px solid var(--border2);
    padding-bottom: 0.75rem;
  }
  .section-head h3 {
    font-size: 0.95rem;
    font-weight: 700;
    color: var(--text);
  }
  .section-sub {
    font-size: 0.8rem;
    color: var(--text2);
    line-height: 1.4;
  }

  /* Storage */
  .info-grid {
    display: grid;
    grid-template-columns: 1fr 2fr;
    gap: 0.35rem 1rem;
    font-size: 0.875rem;
  }
  .info-label {
    color: var(--text2);
  }
  .info-val {
    color: var(--text);
  }
  .mono {
    font-family: monospace;
    font-size: 0.8rem;
  }
  .quota-bar {
    height: 6px;
    background: var(--border);
    border-radius: 3px;
    overflow: hidden;
    margin-top: 0.25rem;
  }
  .quota-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 3px;
    transition: width 0.4s;
  }
  .quota-fill.warn {
    background: var(--warning);
  }
  .quota-fill.crit {
    background: var(--danger);
  }
  .quota-label {
    font-size: 0.75rem;
    color: var(--text3);
  }

  /* API Tokens */
  .token-reveal {
    background: rgba(245, 166, 35, 0.1);
    border: 1px solid var(--warning);
    border-radius: var(--radius);
    padding: 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    font-size: 0.82rem;
    color: var(--warning);
  }
  .token-val {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    background: var(--input-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.4rem 0.6rem;
  }
  .token-val code {
    flex: 1;
    font-size: 0.8rem;
    color: var(--text);
    font-family: monospace;
    word-break: break-all;
  }
  .token-val button {
    background: none;
    border: none;
    color: var(--text2);
    cursor: pointer;
    display: flex;
  }
  .token-val button:hover {
    color: var(--accent);
  }

  .token-create {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .token-create input {
    flex: 1;
    min-width: 160px;
    background: var(--input-bg);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.5rem 0.7rem;
    border-radius: var(--radius);
    font-size: 0.875rem;
  }
  .token-create input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .token-create select {
    background: var(--input-bg);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.5rem 0.7rem;
    border-radius: var(--radius);
    font-size: 0.875rem;
  }

  .token-list {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .token-item {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    background: var(--surface2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.65rem 0.85rem;
  }
  .token-meta {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .token-name {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--text);
  }
  .token-prefix {
    font-size: 0.75rem;
    color: var(--text3);
    font-family: monospace;
    background: var(--bg);
    padding: 0.1rem 0.35rem;
    border-radius: 4px;
  }
  .token-dates {
    font-size: 0.75rem;
    color: var(--text3);
    white-space: nowrap;
    display: flex;
    gap: 0.3rem;
    flex-wrap: wrap;
  }
  .text-warn {
    color: var(--warning);
  }
  .text-muted {
    color: var(--text3);
  }
  .revoke-btn {
    background: none;
    border: none;
    color: var(--text3);
    cursor: pointer;
    display: flex;
    padding: 0.25rem;
    border-radius: 4px;
  }
  .revoke-btn:hover {
    background: var(--danger-bg);
    color: var(--danger);
  }

  .empty-hint {
    font-size: 0.82rem;
    color: var(--text3);
  }

  /* WebDAV */
  .url-box {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    background: var(--input-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.6rem 0.8rem;
  }
  .url-box code {
    flex: 1;
    font-size: 0.82rem;
    color: var(--accent);
    font-family: monospace;
    word-break: break-all;
  }
  .url-box button {
    background: none;
    border: none;
    color: var(--text2);
    cursor: pointer;
    display: flex;
  }
  .url-box button:hover {
    color: var(--accent);
  }

  /* Language */
  select {
    background: var(--input-bg);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.5rem 0.75rem;
    border-radius: var(--radius);
    font-size: 0.875rem;
  }
  select:focus {
    outline: none;
    border-color: var(--accent);
  }

  /* Password / generic form */
  .form-stack {
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }

  input[type='password'],
  input:not([type]) {
    background: var(--input-bg);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.55rem 0.75rem;
    border-radius: var(--radius);
    font-size: 0.875rem;
    width: 100%;
    transition:
      border-color var(--transition),
      box-shadow var(--transition);
  }
  input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  .btn-primary {
    background: var(--accent);
    border: none;
    color: var(--on-accent);
    padding: 0.55rem 1rem;
    border-radius: var(--radius);
    font-size: 0.875rem;
    font-weight: 600;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    transition: background var(--transition);
    white-space: nowrap;
  }
  .btn-primary:hover:not(:disabled) {
    background: var(--accent-h);
  }
  .btn-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .field-label {
    font-size: 0.75rem;
    font-weight: 700;
    color: var(--text3);
    text-transform: none;
    letter-spacing: 0;
    display: block;
    margin-bottom: 0.3rem;
  }

  /* States */
  .state-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.82rem;
    color: var(--text2);
    padding: 0.5rem 0;
  }
  .state-row.danger {
    color: var(--danger);
  }
  .link-btn {
    background: none;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: 0.82rem;
    padding: 0;
  }
  .link-btn:hover {
    text-decoration: underline;
  }

  .spinner {
    width: 14px;
    height: 14px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
    flex-shrink: 0;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (max-width: 600px) {
    .modal-layout {
      grid-template-columns: 1fr;
    }
    .modal-nav {
      flex-direction: row;
      overflow-x: auto;
      border-right: none;
      border-bottom: 1px solid var(--border);
      padding: 0.4rem;
    }
    .nav-item {
      white-space: nowrap;
      width: auto;
      min-height: 44px;
    }
    .modal-body {
      padding: 0.9rem;
    }
    .token-create {
      flex-direction: column;
    }
    .token-dates {
      display: none;
    }
  }
</style>
