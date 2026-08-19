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
  let userFilter = ''
  let usersLoading = true
  let statsLoading = true

  onMount(() => {
    loadUsers()
    loadStats()
  })

  async function loadUsers() {
    usersLoading = true
    const res = await apiFetch('/api/users')
    if (res.ok) users = await res.json() ?? []
    usersLoading = false
  }

  async function loadAudit() {
    const res = await apiFetch('/api/audit?limit=200')
    if (res.ok) audit = await res.json() ?? []
  }

  async function loadStats() {
    statsLoading = true
    const res = await apiFetch('/api/stats')
    if (res.ok) stats = await res.json()
    statsLoading = false
  }

  async function refreshAll() {
    await Promise.all([loadUsers(), loadStats()])
    if (tab === 'audit') await loadAudit()
    success('Admin data refreshed')
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
  $: filteredUsers = users.filter(u => u.username.toLowerCase().includes(userFilter.toLowerCase()) || u.root_path.toLowerCase().includes(userFilter.toLowerCase()))
</script>

<div class="admin">
  <div class="admin-header">
    <span class="brand"><Icon name="folder" size={18} /> Fileship <span class="badge">Admin</span></span>
    <div class="header-actions">
      <button class="refresh" onclick={refreshAll}><Icon name="refresh" size={15} /> Refresh</button>
      <a href="/" class="back"><Icon name="chevron_r" size={14} /> Back to Files</a>
    </div>
  </div>

  <nav class="tabs">
    <button class:active={tab==='users'} onclick={() => tab='users'}><Icon name="users" size={15} /> Users</button>
    <button class:active={tab==='stats'} onclick={() => tab='stats'}><Icon name="bar_chart" size={15} /> Stats</button>
    <button class:active={tab==='audit'} onclick={() => tab='audit'}><Icon name="clipboard" size={15} /> Audit Log</button>
  </nav>

  <div class="tab-content">

    <!-- USERS TAB -->
    {#if tab === 'users'}
      {#if stats}
        <div class="overview-grid">
          <div class="overview-card"><span class="overview-icon"><Icon name="users" size={18} /></span><div><strong>{stats.user_count}</strong><span>Users</span></div></div>
          <div class="overview-card"><span class="overview-icon storage"><Icon name="save" size={18} /></span><div><strong>{formatBytes(stats.disk_usage)}</strong><span>Storage used</span></div></div>
          <div class="overview-card"><span class="overview-icon activity"><Icon name="activity" size={18} /></span><div><strong>{audit.length || '—'}</strong><span>Recent events</span></div></div>
        </div>
      {/if}
      <div class="card">
        <div class="card-heading"><div><h3>Add user</h3><p class="muted">Create an account and assign its home directory.</p></div></div>
        <div class="form-row">
          <input placeholder="Username" minlength="2" bind:value={form.username} />
          <input type="password" placeholder="Password" bind:value={form.password} />
          <input placeholder="Root path" bind:value={form.root_path} />
          <label class="checkbox"><input type="checkbox" bind:checked={form.is_admin} /> Admin</label>
          <button onclick={createUser} disabled={!form.username.trim() || form.password.length < 8}>Add user</button>
        </div>
      </div>

      <div class="card">
        <div class="card-heading"><div><h3>Users <span class="count">{filteredUsers.length}</span></h3><p class="muted">Manage access, storage limits and permissions.</p></div><label class="user-search"><Icon name="search" size={14} /><input placeholder="Filter users" bind:value={userFilter} /></label></div>
        {#if usersLoading}
          <div class="loading-state"><span class="spinner"></span>Loading users...</div>
        {:else if filteredUsers.length === 0}
          <div class="empty-state"><Icon name="users" size={24} /><p>No users match this filter.</p></div>
        {:else}
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
            {#each filteredUsers as u (u.id)}
              <tr>
                <td>{u.username}</td>
                <td class="mono">{u.root_path}</td>
                <td>{#if u.is_admin}<Icon name="check" size={14} />{:else}<span class="muted">No</span>{/if}</td>
                <td>{u.quota_bytes > 0 ? formatBytes(u.quota_bytes) : '∞'}</td>
                <td>{formatBytes(u.disk_usage)}</td>
                <td>{new Date(u.created_at).toLocaleDateString()}</td>
                <td class="row-actions">
                  <button class="icon-btn" onclick={() => { editingQuota = editingQuota === u.id ? null : u.id; quotaForm[u.id] = { quota_mb: Math.round((u.quota_bytes||0)/1024/1024), allowed_types: u.allowed_types||'' } }} title="Quota"><Icon name="settings" size={14} /></button>
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
        {/if}
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
  .admin-header { display: flex; align-items: center; justify-content: space-between; padding: 1.1rem clamp(1rem, 4vw, 3rem); background: var(--surface); border-bottom: 1px solid var(--border); }
  .brand { display: inline-flex; align-items: center; gap: 0.55rem; font-size: 1.2rem; font-weight: 700; }
  .badge { background: var(--row-hover); color: var(--accent); font-size: 0.7rem; padding: 0.25rem 0.55rem; border-radius: 999px; margin-left: 0.35rem; vertical-align: middle; }
  .back { display: inline-flex; align-items: center; gap: 0.35rem; color: var(--accent); text-decoration: none; font-size: 0.85rem; }
  .header-actions { display: flex; align-items: center; gap: 0.75rem; }
  .refresh { display: inline-flex; align-items: center; gap: 0.4rem; background: var(--surface2); color: var(--text); border: 1px solid var(--border); padding: 0.45rem 0.7rem; }
  .refresh:hover { background: var(--row-hover); }
  .tabs { display: flex; gap: 0.25rem; border-bottom: 1px solid var(--border); padding: 0 clamp(1rem, 4vw, 3rem); background: var(--surface); }
  .tabs button { display: inline-flex; align-items: center; gap: 0.45rem; background: none; border: none; color: var(--text2); padding: 0.85rem 1rem; cursor: pointer; font-size: 0.85rem; border-bottom: 2px solid transparent; margin-bottom: -1px; }
  .tabs button.active { color: var(--accent); border-bottom-color: var(--accent); }
  .tabs button:hover { color: var(--text); }
  .tab-content { width: min(100%, 1440px); margin: 0 auto; padding: 2rem clamp(1rem, 4vw, 3rem); display: flex; flex-direction: column; gap: 1.25rem; }
  .card { background: var(--surface); border: 1px solid var(--border); border-radius: 10px; padding: 1.25rem; box-shadow: var(--shadow); overflow-x: auto; }
  .card-heading { display: flex; align-items: center; justify-content: space-between; gap: 1rem; margin-bottom: 1rem; }
  .card-heading h3 { margin-bottom: 0.25rem; }
  .count { color: var(--accent); font-weight: 500; }
  .user-search { display: flex; align-items: center; gap: 0.45rem; border: 1px solid var(--border); border-radius: 6px; padding: 0.35rem 0.55rem; color: var(--text2); }
  .user-search input { border: 0; padding: 0.25rem; width: 170px; background: transparent; }
  .overview-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 1rem; }
  .overview-card { display: flex; align-items: center; gap: 0.8rem; padding: 1rem; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); box-shadow: var(--shadow); }
  .overview-card > div { display: flex; flex-direction: column; gap: 0.15rem; }
  .overview-card strong { font-size: 1.2rem; }
  .overview-card span:last-child { color: var(--text2); font-size: 0.78rem; }
  .overview-icon { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 8px; color: var(--accent); background: var(--row-hover); }
  .overview-icon.storage { color: var(--success); }
  .overview-icon.activity { color: #c58b36; }
  .loading-state, .empty-state { display: flex; align-items: center; justify-content: center; gap: 0.6rem; padding: 3rem 1rem; color: var(--text2); }
  .empty-state { flex-direction: column; }
  .spinner { width: 16px; height: 16px; border: 2px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  h3 { margin: 0 0 1rem; font-size: 0.95rem; color: var(--text); }
  .form-row { display: flex; gap: 0.75rem; flex-wrap: wrap; align-items: center; }
  input:not([type="checkbox"]) { background: var(--bg); border: 1px solid var(--border); color: var(--text); padding: 0.5rem 0.75rem; border-radius: 6px; font-size: 0.9rem; }
  input:focus { outline: none; border-color: var(--accent); }
  .checkbox { display: flex; align-items: center; gap: 0.4rem; font-size: 0.9rem; color: var(--text2); }
  button { background: var(--accent); border: none; color: #fff; padding: 0.5rem 1rem; border-radius: 6px; cursor: pointer; font-size: 0.9rem; }
  button:hover { background: var(--accent-h); }
  button.cancel { background: var(--border); color: var(--text); }
  table { width: 100%; border-collapse: collapse; font-size: 0.875rem; }
  th { text-align: left; padding: 0.65rem 0.75rem; font-size: 0.72rem; color: var(--text2); text-transform: uppercase; letter-spacing: 0.05em; border-bottom: 1px solid var(--border); white-space: nowrap; }
  td { padding: 0.6rem 0.75rem; border-bottom: 1px solid var(--bg); }
  .row-actions { display: flex; gap: 0.25rem; }
  .icon-btn { background: none; border: none; cursor: pointer; padding: 0.2rem 0.4rem; border-radius: 4px; font-size: 1rem; color: var(--text); }
  .icon-btn:hover { background: var(--border); }
  .icon-btn.danger:hover { background: var(--danger-bg); }
  .quota-row td { background: var(--bg); padding: 0; }
  .quota-form { padding: 1rem; display: flex; flex-direction: column; gap: 0.75rem; }
  .quota-form label { display: flex; flex-direction: column; gap: 0.3rem; font-size: 0.85rem; color: var(--text2); }
  .quota-form input { width: 100%; }
  .quota-actions { display: flex; gap: 0.5rem; }
  .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 1rem; }
  .stat-card { background: var(--surface); border: 1px solid var(--border); border-radius: 10px; padding: 1.25rem; text-align: center; box-shadow: var(--shadow); }
  .stat-value { font-size: 1.6rem; font-weight: 700; color: var(--accent); }
  .stat-label { font-size: 0.8rem; color: var(--text2); margin-top: 0.25rem; }
  .user-usage { display: flex; align-items: center; gap: 1rem; padding: 0.4rem 0; }
  .user-name { width: 8rem; flex-shrink: 0; font-size: 0.9rem; }
  .usage-bar-wrap { flex: 1; }
  .usage-bar { height: 6px; background: var(--border); border-radius: 3px; overflow: hidden; }
  .usage-fill { height: 100%; background: var(--accent); border-radius: 3px; }
  .usage-text { font-size: 0.8rem; color: var(--text2); white-space: nowrap; }
  .action-badge { background: var(--bg); border: 1px solid var(--border); padding: 0.1rem 0.4rem; border-radius: 4px; font-size: 0.75rem; font-family: monospace; }
  .path-cell { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mono { font-family: monospace; }
  .small { font-size: 0.8rem; }
  .muted { color: var(--text2); font-size: 0.9rem; }
  @media (max-width: 700px) { .admin-header { align-items: flex-start; gap: 0.75rem; flex-direction: column; } .header-actions { width: 100%; justify-content: space-between; } .tabs { overflow-x: auto; } .form-row { align-items: stretch; flex-direction: column; } .form-row input, .form-row button { width: 100%; } .user-usage { align-items: flex-start; flex-direction: column; gap: 0.35rem; } .user-name { width: auto; } .usage-bar-wrap { width: 100%; } .overview-grid { grid-template-columns: 1fr; } .card-heading { align-items: stretch; flex-direction: column; } .user-search input { width: 100%; } }
</style>
