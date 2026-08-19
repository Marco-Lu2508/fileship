<script>
  import { onMount } from 'svelte'
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import Toast from './Toast.svelte'
  import Icon from './Icon.svelte'

  let tab = 'users'
  let users = [], audit = [], stats = null
  let form = { username: '', password: '', is_admin: false, root_path: '/' }
  let quotaForm = {}, editingQuota = null
  let permForm = {}, editingPerms = null, perms = {}
  let userFilter = ''
  let usersLoading = true, statsLoading = true
  let usersError = '', statsError = ''

  onMount(() => { loadUsers(); loadStats() })

  async function loadUsers() {
    usersLoading = true; usersError = ''
    try {
      const res = await apiFetch('/api/users')
      if (res.ok) users = await res.json() ?? []
      else usersError = `HTTP ${res.status}`
    } catch { usersError = 'Server unreachable' }
    usersLoading = false
  }

  async function loadAudit() {
    const res = await apiFetch('/api/audit?limit=200')
    if (res.ok) audit = await res.json() ?? []
  }

  async function loadStats() {
    statsLoading = true; statsError = ''
    try {
      const res = await apiFetch('/api/stats')
      if (res.ok) stats = await res.json()
      else statsError = `HTTP ${res.status}`
    } catch { statsError = 'Server unreachable' }
    statsLoading = false
  }

  async function loadPerms(userId) {
    const res = await apiFetch(`/api/users/${userId}/permissions`)
    if (res.ok) perms[userId] = await res.json() ?? []
    perms = perms
  }

  async function createUser() {
    const res = await apiFetch('/api/users', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form)
    })
    if (!res.ok) { error(await res.text()); return }
    form = { username: '', password: '', is_admin: false, root_path: '/' }
    success('User created'); await loadUsers()
  }

  async function deleteUser(id) {
    if (!confirm('Delete this user?')) return
    const res = await apiFetch(`/api/users/${id}`, { method: 'DELETE' })
    if (!res.ok) { error(await res.text()); return }
    success('User deleted'); await loadUsers()
  }

  async function saveQuota(id) {
    const q = quotaForm[id] || {}
    const res = await apiFetch(`/api/users/${id}/quota`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ quota_bytes: parseInt(q.quota_mb||0)*1024*1024, allowed_types: q.allowed_types||'' })
    })
    if (!res.ok) { error(await res.text()); return }
    success('Quota saved'); editingQuota = null; await loadUsers()
  }

  async function savePermission(userId) {
    const p = permForm[userId] || {}
    if (!p.path) { error('Path required'); return }
    const res = await apiFetch(`/api/users/${userId}/permissions`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        path: p.path,
        can_read: p.can_read !== false,
        can_write: p.can_write !== false,
        can_delete: p.can_delete !== false,
        can_share: p.can_share !== false,
      })
    })
    if (!res.ok) { error(await res.text()); return }
    success('Permission saved')
    permForm[userId] = {}
    await loadPerms(userId)
  }

  async function deletePermission(userId, permId) {
    await apiFetch(`/api/permissions/${permId}`, { method: 'DELETE' })
    success('Permission removed'); await loadPerms(userId)
  }

  async function refreshAll() {
    await Promise.all([loadUsers(), loadStats()])
    if (tab === 'audit') await loadAudit()
    success('Refreshed')
  }

  function fmt(b) {
    if (!b) return '—'
    const u = ['B','KB','MB','GB']
    const i = Math.floor(Math.log(b) / Math.log(1024))
    return (b / 1024**i).toFixed(1) + ' ' + u[i]
  }

  $: if (tab === 'audit' && audit.length === 0) loadAudit()
  $: filtered = users.filter(u =>
    u.username.toLowerCase().includes(userFilter.toLowerCase()) ||
    u.root_path.toLowerCase().includes(userFilter.toLowerCase()))
</script>

<div class="admin-wrap">
  <!-- Header -->
  <header class="admin-header">
    <div class="header-brand">
      <div class="brand-icon"><Icon name="folder" size={16} /></div>
      <div>
        <strong>Fileship</strong>
        <span class="admin-badge">Admin</span>
      </div>
    </div>
    <div class="header-right">
      <button class="btn-icon" onclick={refreshAll} title="Refresh all">
        <Icon name="refresh" size={15} />
      </button>
      <a class="btn-back" href="/">
        <Icon name="folder" size={14} /> Back to Files
      </a>
    </div>
  </header>

  <!-- Tab bar -->
  <nav class="tab-bar">
    {#each [
      { id:'users',  icon:'users',     label:'Users' },
      { id:'stats',  icon:'bar_chart', label:'Stats' },
      { id:'audit',  icon:'activity',  label:'Audit Log' },
    ] as t}
      <button class="tab-btn" class:active={tab === t.id} onclick={() => tab = t.id}>
        <Icon name={t.icon} size={14} />
        {t.label}
      </button>
    {/each}
  </nav>

  <div class="admin-content">

    <!-- ── USERS ────────────────────────────────── -->
    {#if tab === 'users'}

      {#if stats}
        <div class="kpi-row">
          <div class="kpi"><span class="kpi-val">{stats.user_count}</span><span class="kpi-label">Users</span></div>
          <div class="kpi"><span class="kpi-val">{fmt(stats.disk_usage)}</span><span class="kpi-label">Disk used</span></div>
          <div class="kpi"><span class="kpi-val">{audit.length || '—'}</span><span class="kpi-label">Audit events</span></div>
        </div>
      {/if}

      <!-- Create user -->
      <div class="card">
        <div class="card-head">
          <div>
            <h3>Add User</h3>
            <p class="card-sub">Create an account and assign its home directory.</p>
          </div>
        </div>
        <div class="form-grid">
          <input placeholder="Username" bind:value={form.username} />
          <input type="password" placeholder="Password (min 8 chars)" bind:value={form.password} />
          <input placeholder="Root path (e.g. /data/alice)" bind:value={form.root_path} />
          <label class="check-label">
            <input type="checkbox" bind:checked={form.is_admin} />
            Administrator
          </label>
          <button class="btn-primary"
            onclick={createUser}
            disabled={!form.username.trim() || form.password.length < 8}>
            <Icon name="plus" size={14} /> Add user
          </button>
        </div>
      </div>

      <!-- User list -->
      <div class="card">
        <div class="card-head">
          <div>
            <h3>Users <span class="count-badge">{filtered.length}</span></h3>
          </div>
          <label class="search-label">
            <Icon name="search" size={13} />
            <input placeholder="Filter…" bind:value={userFilter} />
          </label>
        </div>

        {#if usersLoading}
          <div class="state-row"><span class="spinner"></span> Loading…</div>
        {:else if usersError}
          <div class="state-row danger"><Icon name="warning" size={14} /> {usersError}
            <button class="link-btn" onclick={loadUsers}>Retry</button>
          </div>
        {:else if filtered.length === 0}
          <div class="state-row"><Icon name="users" size={18} /> No users match this filter.</div>
        {:else}
          <table>
            <thead>
              <tr>
                <th>Username</th><th>Root</th><th>Admin</th>
                <th>Quota</th><th>Usage</th><th>Created</th><th></th>
              </tr>
            </thead>
            <tbody>
              {#each filtered as u (u.id)}
                <tr>
                  <td class="fw">{u.username}</td>
                  <td class="mono muted">{u.root_path}</td>
                  <td>
                    {#if u.is_admin}
                      <span class="tag accent">Admin</span>
                    {:else}
                      <span class="tag">User</span>
                    {/if}
                  </td>
                  <td class="muted">{u.quota_bytes > 0 ? fmt(u.quota_bytes) : '∞'}</td>
                  <td class="muted">{fmt(u.disk_usage)}</td>
                  <td class="muted">{new Date(u.created_at).toLocaleDateString()}</td>
                  <td class="row-actions">
                    <button class="act-btn" title="Quota & Types"
                      onclick={() => {
                        editingQuota = editingQuota === u.id ? null : u.id
                        quotaForm[u.id] = { quota_mb: Math.round((u.quota_bytes||0)/1024/1024), allowed_types: u.allowed_types||'' }
                        editingPerms = null
                      }}>
                      <Icon name="settings" size={13} />
                    </button>
                    <button class="act-btn" title="Path permissions"
                      onclick={async () => {
                        editingPerms = editingPerms === u.id ? null : u.id
                        editingQuota = null
                        if (editingPerms) await loadPerms(u.id)
                      }}>
                      <Icon name="lock" size={13} />
                    </button>
                    <button class="act-btn danger" title="Delete" onclick={() => deleteUser(u.id)}>
                      <Icon name="trash" size={13} />
                    </button>
                  </td>
                </tr>

                {#if editingQuota === u.id}
                  <tr class="expand-row">
                    <td colspan="7">
                      <div class="expand-body">
                        <strong>Quota & Allowed Types for {u.username}</strong>
                        <div class="expand-form">
                          <label class="field-lbl">Quota (MB, 0 = unlimited)
                            <input type="number" min="0" bind:value={quotaForm[u.id].quota_mb} />
                          </label>
                          <label class="field-lbl">Allowed MIME types (comma-separated, empty = all)
                            <input placeholder="e.g. image/,application/pdf,text/" bind:value={quotaForm[u.id].allowed_types} />
                          </label>
                        </div>
                        <div class="expand-actions">
                          <button class="btn-primary sm" onclick={() => saveQuota(u.id)}>Save</button>
                          <button class="btn-ghost sm" onclick={() => editingQuota = null}>Cancel</button>
                        </div>
                      </div>
                    </td>
                  </tr>
                {/if}

                {#if editingPerms === u.id}
                  <tr class="expand-row">
                    <td colspan="7">
                      <div class="expand-body">
                        <strong>Path Permissions for {u.username}</strong>
                        <p class="card-sub">Rules restrict access to specific paths. Longest prefix wins.</p>

                        {#if perms[u.id]?.length > 0}
                          <div class="perm-list">
                            {#each perms[u.id] as p (p.id)}
                              <div class="perm-item">
                                <code class="perm-path">{p.path}</code>
                                <span class="perm-flags">
                                  {#each [['R','can_read'],['W','can_write'],['D','can_delete'],['S','can_share']] as [lbl, key]}
                                    <span class="perm-flag" class:on={p[key]}>{lbl}</span>
                                  {/each}
                                </span>
                                <button class="act-btn danger" onclick={() => deletePermission(u.id, p.id)}>
                                  <Icon name="trash" size={12} />
                                </button>
                              </div>
                            {/each}
                          </div>
                        {:else}
                          <p class="card-sub">No rules — full access.</p>
                        {/if}

                        <div class="expand-form">
                          <label class="field-lbl">Path (relative, e.g. projects/secret)
                            <input placeholder="path/to/restrict" bind:value={permForm[u.id].path} />
                          </label>
                          <div class="perm-checks">
                            {#each [['Read','can_read'],['Write','can_write'],['Delete','can_delete'],['Share','can_share']] as [lbl, key]}
                              <label class="check-label">
                                <input type="checkbox"
                                  checked={permForm[u.id]?.[key] !== false}
                                  onchange={(e) => { permForm[u.id] = permForm[u.id]||{}; permForm[u.id][key] = e.target.checked }}
                                />
                                {lbl}
                              </label>
                            {/each}
                          </div>
                        </div>
                        <div class="expand-actions">
                          <button class="btn-primary sm" onclick={() => savePermission(u.id)}>Add rule</button>
                          <button class="btn-ghost sm" onclick={() => editingPerms = null}>Close</button>
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

    <!-- ── STATS ───────────────────────────────── -->
    {:else if tab === 'stats'}
      {#if statsError}
        <div class="state-row danger"><Icon name="warning" size={14} /> {statsError}
          <button class="link-btn" onclick={loadStats}>Retry</button>
        </div>
      {:else if stats}
        <div class="kpi-row">
          <div class="kpi lg"><span class="kpi-val">{stats.user_count}</span><span class="kpi-label">Total Users</span></div>
          <div class="kpi lg"><span class="kpi-val">{fmt(stats.disk_usage)}</span><span class="kpi-label">Total Disk Usage</span></div>
          <div class="kpi lg mono"><span class="kpi-val sm">{stats.root_path}</span><span class="kpi-label">Root Path</span></div>
        </div>

        {#if stats.users?.length > 0}
          <div class="card">
            <h3>Per-User Disk Usage</h3>
            <div class="usage-list">
              {#each stats.users as u}
                {@const pct = u.quota_bytes > 0 ? Math.min(100, Math.round(u.disk_usage/u.quota_bytes*100)) : 0}
                <div class="usage-row">
                  <span class="usage-name">{u.username}</span>
                  <div class="usage-bar">
                    <div class="usage-fill" style="width:{pct}%"
                      class:warn={pct>80} class:crit={pct>95}></div>
                  </div>
                  <span class="usage-val">{fmt(u.disk_usage)}{u.quota_bytes > 0 ? ' / ' + fmt(u.quota_bytes) : ''}</span>
                </div>
              {/each}
            </div>
          </div>
        {/if}
      {:else}
        <div class="state-row"><span class="spinner"></span> Loading…</div>
      {/if}

    <!-- ── AUDIT ───────────────────────────────── -->
    {:else if tab === 'audit'}
      <div class="card">
        <div class="card-head">
          <h3>Audit Log <span class="count-badge">{audit.length}</span></h3>
          <button class="btn-icon" onclick={loadAudit} title="Reload"><Icon name="refresh" size={14} /></button>
        </div>
        {#if audit.length === 0}
          <div class="state-row"><span class="spinner"></span> Loading…</div>
        {:else}
          <table>
            <thead>
              <tr><th>Time</th><th>User</th><th>Action</th><th>Path</th><th>IP</th></tr>
            </thead>
            <tbody>
              {#each audit as e (e.id)}
                <tr>
                  <td class="mono muted sm">{new Date(e.created_at).toLocaleString()}</td>
                  <td class="fw">{e.username || e.user_id}</td>
                  <td><span class="tag">{e.action}</span></td>
                  <td class="mono muted sm path-cell">{e.path}</td>
                  <td class="mono muted sm">{e.ip}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </div>
    {/if}

  </div>
</div>

<Toast />

<style>
  .admin-wrap {
    min-height: 100vh;
    background: var(--bg);
    color: var(--text);
    display: flex;
    flex-direction: column;
  }

  /* Header */
  .admin-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 0.85rem 1.5rem;
    background: var(--header-bg);
    border-bottom: 1px solid var(--border);
  }

  .header-brand {
    display: flex; align-items: center; gap: 0.7rem;
  }

  .brand-icon {
    width: 30px; height: 30px; border-radius: var(--radius);
    background: var(--accent); color: #fff;
    display: grid; place-items: center;
  }

  .header-brand strong { font-size: 0.95rem; font-weight: 700; }

  .admin-badge {
    display: inline-block;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 0.65rem; font-weight: 700;
    padding: 0.15rem 0.5rem; border-radius: 20px;
    margin-left: 0.4rem; vertical-align: middle;
    text-transform: uppercase; letter-spacing: 0.06em;
  }

  .header-right { display: flex; align-items: center; gap: 0.5rem; }

  .btn-icon {
    background: none; border: 1px solid var(--border); color: var(--text2);
    cursor: pointer; padding: 0.4rem; border-radius: var(--radius);
    display: flex; transition: background var(--transition);
  }
  .btn-icon:hover { background: var(--row-hover); color: var(--text); }

  .btn-back {
    display: inline-flex; align-items: center; gap: 0.35rem;
    background: var(--surface); border: 1px solid var(--border);
    color: var(--text2); padding: 0.4rem 0.75rem;
    border-radius: var(--radius); font-size: 0.82rem;
    text-decoration: none; transition: background var(--transition);
  }
  .btn-back:hover { background: var(--row-hover); color: var(--text); text-decoration: none; }

  /* Tabs */
  .tab-bar {
    display: flex; gap: 0.1rem;
    padding: 0 1.5rem;
    background: var(--header-bg);
    border-bottom: 1px solid var(--border);
  }

  .tab-btn {
    display: inline-flex; align-items: center; gap: 0.4rem;
    background: none; border: none; color: var(--text2);
    padding: 0.75rem 1rem; cursor: pointer;
    font-size: 0.85rem; border-bottom: 2px solid transparent;
    margin-bottom: -1px; transition: color var(--transition);
  }
  .tab-btn:hover { color: var(--text); }
  .tab-btn.active { color: var(--accent); border-bottom-color: var(--accent); }

  /* Content */
  .admin-content {
    flex: 1;
    padding: 1.5rem;
    max-width: 1280px;
    margin: 0 auto;
    width: 100%;
    display: flex; flex-direction: column; gap: 1.25rem;
  }

  /* KPI row */
  .kpi-row { display: flex; gap: 1rem; flex-wrap: wrap; }

  .kpi {
    flex: 1; min-width: 140px;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-lg); padding: 1.1rem 1.25rem;
    display: flex; flex-direction: column; gap: 0.25rem;
    box-shadow: var(--shadow-sm);
  }

  .kpi-val { font-size: 1.5rem; font-weight: 700; color: var(--text); }
  .kpi-val.sm { font-size: 0.85rem; font-family: monospace; }
  .kpi-label { font-size: 0.75rem; color: var(--text3); text-transform: uppercase; letter-spacing: 0.06em; }
  .kpi.lg .kpi-val { font-size: 1.75rem; color: var(--accent); }
  .kpi.mono .kpi-val { font-family: monospace; }

  /* Card */
  .card {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-lg); overflow: hidden;
    box-shadow: var(--shadow-sm);
  }

  .card-head {
    display: flex; align-items: center; justify-content: space-between;
    padding: 1rem 1.25rem; border-bottom: 1px solid var(--border);
    gap: 1rem; flex-wrap: wrap;
  }

  .card-head h3 { margin: 0; font-size: 0.9rem; font-weight: 700; color: var(--text); }
  .card-sub { font-size: 0.78rem; color: var(--text2); margin-top: 0.15rem; }

  .count-badge {
    background: var(--surface2); color: var(--text2);
    font-size: 0.72rem; padding: 0.1rem 0.45rem;
    border-radius: 20px; font-weight: 600; margin-left: 0.3rem;
  }

  h3 { padding: 1rem 1.25rem 0; font-size: 0.9rem; font-weight: 700; margin: 0; }

  /* Form */
  .form-grid {
    display: flex; gap: 0.6rem; flex-wrap: wrap;
    align-items: center; padding: 1rem 1.25rem;
  }

  .form-grid input:not([type='checkbox']) { flex: 1; min-width: 140px; }

  input:not([type='checkbox']),
  input[type='number'] {
    background: var(--input-bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.5rem 0.7rem;
    border-radius: var(--radius); font-size: 0.875rem;
    transition: border-color var(--transition), box-shadow var(--transition);
  }
  input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }

  .check-label {
    display: flex; align-items: center; gap: 0.35rem;
    font-size: 0.85rem; color: var(--text2); cursor: pointer;
    white-space: nowrap;
  }
  .check-label input { accent-color: var(--accent); width: auto; }

  .btn-primary {
    display: inline-flex; align-items: center; gap: 0.35rem;
    background: var(--accent); border: none; color: #fff;
    padding: 0.5rem 0.9rem; border-radius: var(--radius);
    font-size: 0.875rem; font-weight: 600; cursor: pointer;
    transition: background var(--transition);
    white-space: nowrap;
  }
  .btn-primary:hover:not(:disabled) { background: var(--accent-h); }
  .btn-primary:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-primary.sm { padding: 0.4rem 0.7rem; font-size: 0.82rem; }

  .btn-ghost {
    background: var(--surface2); border: 1px solid var(--border);
    color: var(--text2); padding: 0.4rem 0.7rem;
    border-radius: var(--radius); cursor: pointer; font-size: 0.82rem;
    transition: background var(--transition);
  }
  .btn-ghost:hover { background: var(--row-hover); color: var(--text); }
  .btn-ghost.sm { padding: 0.35rem 0.6rem; font-size: 0.78rem; }

  /* Search in card header */
  .search-label {
    display: flex; align-items: center; gap: 0.4rem;
    background: var(--input-bg); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 0.35rem 0.6rem;
    color: var(--text2); flex-shrink: 0;
  }
  .search-label input {
    border: none; background: none; color: var(--text);
    padding: 0; font-size: 0.82rem; width: 160px;
    box-shadow: none;
  }
  .search-label input:focus { outline: none; box-shadow: none; border: none; }

  /* Table */
  table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
  thead { background: var(--surface2); }
  th {
    text-align: left; padding: 0.6rem 1rem;
    font-size: 0.68rem; color: var(--text3);
    font-weight: 700; text-transform: uppercase; letter-spacing: 0.07em;
    border-bottom: 1px solid var(--border); white-space: nowrap;
  }
  td { padding: 0.65rem 1rem; border-bottom: 1px solid var(--border2); vertical-align: middle; }
  tr:last-child td { border-bottom: none; }
  tr:hover td { background: var(--row-hover); }

  .fw { font-weight: 600; color: var(--text); }
  .mono { font-family: monospace; }
  .muted { color: var(--text2); }
  .sm { font-size: 0.78rem; }

  .tag {
    display: inline-flex; padding: 0.15rem 0.5rem;
    border-radius: 20px; font-size: 0.72rem; font-weight: 600;
    background: var(--surface2); color: var(--text2);
    border: 1px solid var(--border);
  }
  .tag.accent { background: var(--accent-soft); color: var(--accent); border-color: transparent; }

  .row-actions { display: flex; gap: 0.25rem; }

  .act-btn {
    background: none; border: none; color: var(--text3);
    cursor: pointer; padding: 0.3rem; border-radius: 5px;
    display: grid; place-items: center;
    transition: background var(--transition), color var(--transition);
  }
  .act-btn:hover { background: var(--row-hover); color: var(--text); }
  .act-btn.danger:hover { background: var(--danger-bg); color: var(--danger); }

  .path-cell { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  /* Expand rows */
  .expand-row td { padding: 0; background: var(--bg); }
  .expand-body {
    padding: 1rem 1.25rem;
    display: flex; flex-direction: column; gap: 0.75rem;
    border-top: 1px solid var(--border);
    animation: expand-in 0.15s ease both;
  }
  @keyframes expand-in {
    from { opacity: 0; transform: translateY(-4px); }
    to   { opacity: 1; transform: none; }
  }

  .expand-form { display: flex; flex-direction: column; gap: 0.6rem; }
  .field-lbl { display: flex; flex-direction: column; gap: 0.3rem; font-size: 0.82rem; color: var(--text2); }
  .field-lbl input { margin-top: 0.1rem; }
  .expand-actions { display: flex; gap: 0.5rem; }

  /* Permissions */
  .perm-list { display: flex; flex-direction: column; gap: 0.35rem; }
  .perm-item {
    display: flex; align-items: center; gap: 0.65rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 0.45rem 0.75rem;
  }
  .perm-path { flex: 1; font-size: 0.8rem; color: var(--accent); font-family: monospace; }
  .perm-flags { display: flex; gap: 0.25rem; }
  .perm-flag {
    width: 22px; height: 22px; border-radius: 4px;
    display: grid; place-items: center; font-size: 0.65rem; font-weight: 800;
    background: var(--surface2); color: var(--text3); border: 1px solid var(--border);
  }
  .perm-flag.on { background: var(--success-bg); color: var(--success); border-color: transparent; }

  .perm-checks { display: flex; gap: 1rem; flex-wrap: wrap; }

  /* Stats */
  .usage-list { padding: 0.5rem 1.25rem 1rem; display: flex; flex-direction: column; gap: 0.5rem; }
  .usage-row { display: flex; align-items: center; gap: 0.75rem; }
  .usage-name { width: 9rem; font-size: 0.875rem; font-weight: 600; flex-shrink: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .usage-bar { flex: 1; height: 6px; background: var(--border); border-radius: 3px; overflow: hidden; }
  .usage-fill { height: 100%; background: var(--accent); border-radius: 3px; transition: width 0.4s; }
  .usage-fill.warn { background: var(--warning); }
  .usage-fill.crit { background: var(--danger); }
  .usage-val { font-size: 0.78rem; color: var(--text2); white-space: nowrap; }

  /* State rows */
  .state-row {
    display: flex; align-items: center; gap: 0.5rem;
    padding: 2.5rem 1.25rem; color: var(--text2);
    font-size: 0.85rem; justify-content: center;
  }
  .state-row.danger { color: var(--danger); }

  .link-btn { background: none; border: none; color: var(--accent); cursor: pointer; font-size: 0.82rem; margin-left: 0.25rem; }
  .link-btn:hover { text-decoration: underline; }

  .spinner {
    width: 16px; height: 16px;
    border: 2px solid var(--border); border-top-color: var(--accent);
    border-radius: 50%; animation: spin 0.7s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  @media (max-width: 700px) {
    .admin-header { flex-wrap: wrap; gap: 0.5rem; }
    .header-right { width: 100%; justify-content: space-between; }
    .tab-bar { overflow-x: auto; }
    .kpi-row { flex-direction: column; }
    .form-grid { flex-direction: column; }
    .form-grid input { width: 100%; }
    .usage-name { width: auto; }
  }
</style>
