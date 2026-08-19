<script>
  import { onMount } from 'svelte'
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import Toast from './Toast.svelte'
  import Icon from './Icon.svelte'

  let tab = 'users'
  let users = []
  let audit = []
  let stats = null
  let form = { username: '', password: '', is_admin: false, root_path: '/' }
  let quotaForm = {}
  let editingQuota = null

  onMount(() => {
    loadUsers()
    loadStats()
  })

  async function loadUsers() {
    const res = await apiFetch('/api/users')
    if (res.ok) users = await res.json() ?? []
  }

  async function loadAudit() {
    const res = await apiFetch('/api/audit?limit=200')
    if (res.ok) audit = await res.json() ?? []
  }

  async function loadStats() {
    const res = await apiFetch('/api/stats')
    if (res.ok) stats = await res.json()
  }

  async function createUser() {
    const res = await apiFetch('/api/users', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form)
    })
    if (!res.ok) { error(await res.text()); return }
    form = { username: '', password: '', is_admin: false, root_path: '/' }
    success('User created')
    await loadUsers()
  }

  async function deleteUser(id) {
    if (!confirm('Delete this user?')) return
    await apiFetch(`/api/users/${id}`, { method: 'DELETE' })
    success('User deleted')
    await loadUsers()
  }

  async function saveQuota(id) {
    const q = quotaForm[id] || {}
    await apiFetch(`/api/users/${id}/quota`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        quota_bytes: parseInt(q.quota_mb || 0) * 1024 * 1024,
        allowed_types: q.allowed_types || ''
      })
    })
    success('Quota saved')
    editingQuota = null
    await loadUsers()
  }

  async function updateUser(id, patch) {
    await apiFetch(`/api/users/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch)
    })
    success('User updated')
    await loadUsers()
  }

  function formatBytes(b) {
    if (!b) return '—'
    const units = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(b) / Math.log(1024))
    return (b / Math.pow(1024, i)).toFixed(1) + ' ' + units[i]
  }

  $: if (tab === 'audit' && audit.length === 0) loadAudit()
</script>

<div class="admin">
  <div class="admin-header">
    <span class="brand"><Icon name="folder" size={18} /> Fileship <span class="badge">Admin</span></span>
    <a href="/" class="back">← Back to Files</a>
  </div>

  <nav class="tabs">
    <button class:active={tab==='users'} onclick={() => tab='users'}><Icon name="users" size={15} /> Users</button>
    <button class:active={tab==='stats'} onclick={() => tab='stats'}><Icon name="bar_chart" size={15} /> Stats</button>
    <button class:active={tab==='audit'} onclick={() => tab='audit'}><Icon name="clipboard" size={15} /> Audit Log</button>
  </nav>

  <div class="tab-content">

    <!-- USERS TAB -->
    {#if tab === 'users'}
      <div class="card">
        <h3>Add User</h3>
        <div class="form-row">
          <input placeholder="Username" bind:value={form.username} />
          <input type="password" placeholder="Password" bind:value={form.password} />
          <input placeholder="Root path" bind:value={form.root_path} />
          <label class="checkbox"><input type="checkbox" bind:checked={form.is_admin} /> Admin</label>
          <button onclick={createUser}>Add</button>
        </div>
      </div>

      <div class="card">
        <h3>Users ({users.length})</h3>
        <table>
          <thead>
            <tr>
              <th>Username</th>
              <th>Root</th>
              <th>Admin</th>
              <th>Quota</th>
              <th>Disk Usage</th>
              <th>Created</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {#each users as u (u.id)}
              <tr>
                <td>{u.username}</td>
                <td class="mono">{u.root_path}</td>
                <td>{u.is_admin ? '✅' : '—'}</td>
                <td>{u.quota_bytes > 0 ? formatBytes(u.quota_bytes) : '∞'}</td>
                <td>{formatBytes(u.disk_usage)}</td>
                <td>{new Date(u.created_at).toLocaleDateString()}</td>
                <td class="row-actions">
                  <button class="icon-btn" onclick={() => { editingQuota = editingQuota === u.id ? null : u.id; quotaForm[u.id] = { quota_mb: Math.round((u.quota_bytes||0)/1024/1024), allowed_types: u.allowed_types||'' } }} title="Quota">⚙️</button>
                  <button class="icon-btn danger" onclick={() => deleteUser(u.id)} title="Delete"><Icon name="trash" size={14} /></button>
                </td>
              </tr>
              {#if editingQuota === u.id}
                <tr class="quota-row">
                  <td colspan="7">
                    <div class="quota-form">
                      <label>
                        Quota (MB, 0 = unlimited)
                        <input type="number" min="0" bind:value={quotaForm[u.id].quota_mb} />
                      </label>
                      <label>
                        Allowed MIME types (comma-separated, e.g. image/,application/pdf)
                        <input placeholder="Leave empty for all types" bind:value={quotaForm[u.id].allowed_types} />
                      </label>
                      <div class="quota-actions">
                        <button onclick={() => saveQuota(u.id)}>Save</button>
                        <button class="cancel" onclick={() => editingQuota = null}>Cancel</button>
                      </div>
                    </div>
                  </td>
                </tr>
              {/if}
            {/each}
          </tbody>
        </table>
      </div>

    <!-- STATS TAB -->
    {:else if tab === 'stats'}
      {#if stats}
        <div class="stats-grid">
          <div class="stat-card">
            <div class="stat-value">{stats.user_count}</div>
            <div class="stat-label">Users</div>
          </div>
          <div class="stat-card">
            <div class="stat-value">{formatBytes(stats.disk_usage)}</div>
            <div class="stat-label">Total Disk Usage</div>
          </div>
          <div class="stat-card">
            <div class="stat-value mono">{stats.root_path}</div>
            <div class="stat-label">Root Path</div>
          </div>
        </div>

        {#if stats.users}
          <div class="card">
            <h3>Per-User Disk Usage</h3>
            {#each stats.users as u}
              <div class="user-usage">
                <span class="user-name">{u.username}</span>
                <div class="usage-bar-wrap">
                  <div class="usage-bar">
                    <div
                      class="usage-fill"
                      style="width:{u.quota_bytes > 0 ? Math.min(100, Math.round(u.disk_usage/u.quota_bytes*100)) : 0}%"
                    ></div>
                  </div>
                </div>
                <span class="usage-text">{formatBytes(u.disk_usage)} {u.quota_bytes > 0 ? '/ ' + formatBytes(u.quota_bytes) : ''}</span>
              </div>
            {/each}
          </div>
        {/if}
      {:else}
        <p class="muted">Loading…</p>
      {/if}

    <!-- AUDIT TAB -->
    {:else if tab === 'audit'}
      <div class="card">
        <h3>Audit Log (last 200 entries)</h3>
        <table>
          <thead>
            <tr><th>Time</th><th>User</th><th>Action</th><th>Path</th><th>IP</th></tr>
          </thead>
          <tbody>
            {#each audit as e (e.id)}
              <tr>
                <td class="mono small">{new Date(e.created_at).toLocaleString()}</td>
                <td>{e.username || e.user_id}</td>
                <td><span class="action-badge">{e.action}</span></td>
                <td class="mono small path-cell">{e.path}</td>
                <td class="mono small">{e.ip}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

  </div>
</div>

<Toast />

<style>
  .admin { min-height: 100vh; background: var(--bg); color: var(--text); }
  .admin-header { display: flex; align-items: center; justify-content: space-between; padding: 1rem 2rem; background: var(--surface); border-bottom: 1px solid var(--border); }
  .brand { font-size: 1.2rem; font-weight: 700; }
  .badge { background: var(--accent); color: #fff; font-size: 0.7rem; padding: 0.15rem 0.5rem; border-radius: 4px; margin-left: 0.5rem; vertical-align: middle; }
  .back { color: var(--accent); text-decoration: none; font-size: 0.9rem; }
  .tabs { display: flex; gap: 0; border-bottom: 1px solid var(--border); padding: 0 2rem; background: var(--surface); }
  .tabs button { background: none; border: none; color: var(--muted); padding: 0.75rem 1.25rem; cursor: pointer; font-size: 0.9rem; border-bottom: 2px solid transparent; margin-bottom: -1px; }
  .tabs button.active { color: var(--accent); border-bottom-color: var(--accent); }
  .tabs button:hover { color: var(--text); }
  .tab-content { padding: 1.5rem 2rem; display: flex; flex-direction: column; gap: 1.5rem; }
  .card { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 1.25rem; }
  h3 { margin: 0 0 1rem; font-size: 0.95rem; color: var(--text); }
  .form-row { display: flex; gap: 0.75rem; flex-wrap: wrap; align-items: center; }
  input:not([type="checkbox"]) { background: var(--bg); border: 1px solid var(--border); color: var(--text); padding: 0.5rem 0.75rem; border-radius: 6px; font-size: 0.9rem; }
  input:focus { outline: none; border-color: var(--accent); }
  .checkbox { display: flex; align-items: center; gap: 0.4rem; font-size: 0.9rem; color: var(--muted); }
  button { background: var(--accent); border: none; color: #fff; padding: 0.5rem 1rem; border-radius: 6px; cursor: pointer; font-size: 0.9rem; }
  button:hover { background: var(--accent-h); }
  button.cancel { background: var(--border); color: var(--text); }
  table { width: 100%; border-collapse: collapse; font-size: 0.875rem; }
  th { text-align: left; padding: 0.5rem 0.75rem; font-size: 0.75rem; color: var(--muted); text-transform: uppercase; border-bottom: 1px solid var(--border); }
  td { padding: 0.6rem 0.75rem; border-bottom: 1px solid var(--bg); }
  .row-actions { display: flex; gap: 0.25rem; }
  .icon-btn { background: none; border: none; cursor: pointer; padding: 0.2rem 0.4rem; border-radius: 4px; font-size: 1rem; color: var(--text); }
  .icon-btn:hover { background: var(--border); }
  .icon-btn.danger:hover { background: var(--danger-bg); }
  .quota-row td { background: var(--bg); padding: 0; }
  .quota-form { padding: 1rem; display: flex; flex-direction: column; gap: 0.75rem; }
  .quota-form label { display: flex; flex-direction: column; gap: 0.3rem; font-size: 0.85rem; color: var(--muted); }
  .quota-form input { width: 100%; }
  .quota-actions { display: flex; gap: 0.5rem; }
  .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 1rem; }
  .stat-card { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 1.25rem; text-align: center; }
  .stat-value { font-size: 1.6rem; font-weight: 700; color: var(--accent); }
  .stat-label { font-size: 0.8rem; color: var(--muted); margin-top: 0.25rem; }
  .user-usage { display: flex; align-items: center; gap: 1rem; padding: 0.4rem 0; }
  .user-name { width: 8rem; flex-shrink: 0; font-size: 0.9rem; }
  .usage-bar-wrap { flex: 1; }
  .usage-bar { height: 6px; background: var(--border); border-radius: 3px; overflow: hidden; }
  .usage-fill { height: 100%; background: var(--accent); border-radius: 3px; }
  .usage-text { font-size: 0.8rem; color: var(--muted); white-space: nowrap; }
  .action-badge { background: var(--bg); border: 1px solid var(--border); padding: 0.1rem 0.4rem; border-radius: 4px; font-size: 0.75rem; font-family: monospace; }
  .path-cell { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mono { font-family: monospace; }
  .small { font-size: 0.8rem; }
  .muted { color: var(--muted); font-size: 0.9rem; }
</style>
