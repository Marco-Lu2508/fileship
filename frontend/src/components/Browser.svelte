<script>
  import { onMount } from 'svelte'
  import { files, loading, currentPath, selected, loadFiles, mkdir, deleteFile } from '../stores/files.js'
  import { user, logout, apiFetch } from '../stores/auth.js'
  import { connectWS, disconnectWS } from '../stores/ws.js'
  import { success, error } from '../stores/toast.js'
  import { get } from 'svelte/store'
  import Breadcrumb  from './Breadcrumb.svelte'
  import Dropzone    from './Dropzone.svelte'
  import FileRow     from './FileRow.svelte'
  import Preview     from './Preview.svelte'
  import ShareDialog from './ShareDialog.svelte'
  import Toast       from './Toast.svelte'
  import Skeleton    from './Skeleton.svelte'
  import Editor      from './Editor.svelte'
  import Settings    from './Settings.svelte'
  import ThemePicker from './ThemePicker.svelte'
  import Icon        from './Icon.svelte'

  let showNewFolder  = false
  let newFolderName  = ''
  let sortKey        = 'name'
  let sortAsc        = true
  let searchQuery    = ''
  let searchResults  = null
  let searchLoading  = false
  let previewFile    = null
  let shareFile      = null
  let editorFile     = null
  let showSettings   = false
  let showThemePicker = false
  let searchTimeout  = null
  let dragOverPath   = null

  onMount(() => {
    loadFiles('')
    connectWS()
    return disconnectWS
  })

  function onKeydown(e) {
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return
    if (e.ctrlKey || e.metaKey) {
      if (e.key === 'n') { e.preventDefault(); showNewFolder = true }
      if (e.key === 'a') { e.preventDefault(); selected.set(new Set(displayFiles.map(f => f.path))) }
    }
    if (e.key === 'Delete' && get(selected).size > 0) deleteSelected()
    if (e.key === 'Escape') { selected.set(new Set()); searchQuery = ''; searchResults = null; showNewFolder = false }
    if (e.key === 'r' && !e.ctrlKey && !e.metaKey) loadFiles(get(currentPath))
  }

  async function handleMkdir() {
    if (!newFolderName.trim()) return
    try {
      const path = get(currentPath)
      await mkdir(path ? path + '/' + newFolderName : newFolderName)
      await loadFiles(path)
      success(`Folder "${newFolderName}" created`)
    } catch { error('Could not create folder') }
    newFolderName = ''
    showNewFolder = false
  }

  async function deleteSelected() {
    const paths = [...get(selected)]
    if (!paths.length || !confirm(`Delete ${paths.length} item(s)?`)) return
    for (const p of paths) await deleteFile(p)
    await loadFiles(get(currentPath))
    success(`Deleted ${paths.length} item(s)`)
  }

  async function downloadSelected() {
    const paths = [...get(selected)]
    if (!paths.length) return
    const token = localStorage.getItem('access_token')
    const res = await fetch('/api/files/zip-multi', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body: JSON.stringify({ paths })
    })
    if (!res.ok) { error('Download failed'); return }
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url; a.download = 'download.zip'; a.click()
    URL.revokeObjectURL(url)
  }

  async function handleDragOver(e, targetPath) { e.preventDefault(); dragOverPath = targetPath }

  async function handleDrop(e, targetPath) {
    e.preventDefault(); dragOverPath = null
    const srcPath = e.dataTransfer.getData('fileship/path')
    if (!srcPath || srcPath === targetPath) return
    const name = srcPath.split('/').pop()
    const dst  = targetPath ? targetPath + '/' + name : name
    const res  = await apiFetch('/api/files/move', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ src: srcPath, dst })
    })
    if (res.ok) { await loadFiles(get(currentPath)); success(`Moved`) }
    else error('Move failed')
  }

  function setSort(key) {
    sortAsc = sortKey === key ? !sortAsc : true
    sortKey = key
  }

  function onSearch() {
    clearTimeout(searchTimeout)
    if (!searchQuery.trim()) { searchResults = null; return }
    searchLoading = true
    searchTimeout = setTimeout(async () => {
      const res = await apiFetch(`/api/files/search?q=${encodeURIComponent(searchQuery)}`)
      if (res.ok) searchResults = await res.json()
      searchLoading = false
    }, 350)
  }

  $: displayFiles = searchResults !== null ? searchResults : ($files?.files || [])
  $: sorted = [...displayFiles].sort((a, b) => {
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
    let av = a[sortKey], bv = b[sortKey]
    if (typeof av === 'string') av = av.toLowerCase()
    if (typeof bv === 'string') bv = bv.toLowerCase()
    return sortAsc ? (av > bv ? 1 : -1) : (av < bv ? 1 : -1)
  })
  $: totalFiles = $files?.total ?? 0
</script>

<svelte:window onkeydown={onKeydown} />

<div class="layout">
  <!-- Sidebar -->
  <aside class="sidebar">
    <div class="sidebar-brand">
      <Icon name="home" size={16} />
      <span>Fileship</span>
    </div>
    <nav class="sidebar-nav">
      <button class="nav-item active" onclick={() => loadFiles('')}>
        <Icon name="folder" size={15} />
        <span>Files</span>
      </button>
      {#if $user?.is_admin}
        <a class="nav-item" href="/admin">
          <Icon name="users" size={15} />
          <span>Admin</span>
        </a>
      {/if}
      <button class="nav-item" onclick={() => showSettings = true}>
        <Icon name="settings" size={15} />
        <span>Settings</span>
      </button>
    </nav>
    <div class="sidebar-footer">
      <div class="user-row">
        <Icon name="user" size={14} />
        <span class="username">{$user?.username}</span>
      </div>
      <button class="icon-btn" onclick={() => showThemePicker = !showThemePicker} title="Theme">
        <Icon name="palette" size={14} />
      </button>
      <button class="icon-btn" onclick={logout} title="Sign out">
        <Icon name="logout" size={14} />
      </button>
    </div>
  </aside>

  <!-- Main -->
  <div class="main">
    <!-- Toolbar -->
    <div class="toolbar">
      <Breadcrumb path={$currentPath} />
      <div class="toolbar-right">
        <div class="search-wrap">
          <span class="search-icon"><Icon name="search" size={14} /></span>
          <input
            class="search"
            placeholder="Search..."
            bind:value={searchQuery}
            oninput={onSearch}
            aria-label="Search files"
          />
        </div>
        <div class="actions">
          {#if $selected.size > 0}
            <button class="btn" onclick={downloadSelected} title="Download as ZIP">
              <Icon name="download" size={14} />
              <span>ZIP ({$selected.size})</span>
            </button>
            <button class="btn danger" onclick={deleteSelected}>
              <Icon name="trash" size={14} />
              <span>Delete ({$selected.size})</span>
            </button>
          {/if}
          <button class="btn primary" onclick={() => showNewFolder = !showNewFolder} title="New Folder (Ctrl+N)">
            <Icon name="plus" size={14} />
            <span>New Folder</span>
          </button>
        </div>
      </div>
    </div>

    <!-- New Folder Input -->
    {#if showNewFolder}
      <div class="new-folder-bar">
        <Icon name="folder" size={14} />
        <input
          placeholder="Folder name"
          bind:value={newFolderName}
          onkeydown={(e) => { if (e.key === 'Enter') handleMkdir(); if (e.key === 'Escape') showNewFolder = false }}
        />
        <button class="btn primary" onclick={handleMkdir}>Create</button>
        <button class="btn" onclick={() => showNewFolder = false}>Cancel</button>
      </div>
    {/if}

    <Dropzone />

    <!-- File Table -->
    <div class="table-wrap">
      {#if $loading || searchLoading}
        <Skeleton rows={12} />
      {:else if sorted.length === 0}
        <div class="empty">
          <Icon name="folder" size={32} />
          <p>{searchQuery ? `No results for "${searchQuery}"` : 'This folder is empty'}</p>
        </div>
      {:else}
        <div class="table-meta">
          <span>
            {#if searchResults !== null}
              {sorted.length} result(s) for "{searchQuery}"
            {:else}
              {sorted.length} of {totalFiles} items
            {/if}
          </span>
          <span class="shortcuts hide-mobile">Ctrl+A · Del · Esc · R</span>
        </div>
        <table>
          <thead>
            <tr>
              <th class="col-check">
                <input
                  type="checkbox"
                  onchange={(e) => selected.set(e.target.checked ? new Set(sorted.map(f => f.path)) : new Set())}
                  checked={$selected.size === sorted.length && sorted.length > 0}
                  aria-label="Select all"
                />
              </th>
              <th onclick={() => setSort('name')} class="sortable">
                Name
                {#if sortKey === 'name'}<Icon name={sortAsc ? 'sort_up' : 'sort_dn'} size={12} />{/if}
              </th>
              <th onclick={() => setSort('size')} class="sortable">
                Size
                {#if sortKey === 'size'}<Icon name={sortAsc ? 'sort_up' : 'sort_dn'} size={12} />{/if}
              </th>
              <th onclick={() => setSort('mod_time')} class="sortable hide-mobile">
                Modified
                {#if sortKey === 'mod_time'}<Icon name={sortAsc ? 'sort_up' : 'sort_dn'} size={12} />{/if}
              </th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each sorted as file (file.path)}
              <FileRow
                {file}
                {dragOverPath}
                onPreview={(f) => previewFile = f}
                onShare={(f) => shareFile = f}
                onEdit={(f) => editorFile = f}
                onUnzip={async (f) => {
                  const dest = f.path.replace(/\.zip$/i, '')
                  const res = await apiFetch('/api/files/unzip', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ zip_path: f.path, dest_dir: dest })
                  })
                  if (res.ok) { success('Extracted'); await loadFiles(get(currentPath)) }
                  else error('Extract failed')
                }}
                onDragOver={handleDragOver}
                onDrop={handleDrop}
              />
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  </div>
</div>

<Preview    file={previewFile} onClose={() => previewFile = null} />
<ShareDialog file={shareFile} onClose={() => shareFile = null} />
<Editor     file={editorFile} onClose={() => editorFile = null} />
{#if showSettings}   <Settings    onClose={() => showSettings = false} />{/if}
{#if showThemePicker}<ThemePicker onClose={() => showThemePicker = false} />{/if}
<Toast />

<style>
  .layout {
    display: flex; min-height: 100vh;
    background: var(--bg); color: var(--text);
  }

  /* Sidebar */
  .sidebar {
    width: 200px; flex-shrink: 0;
    background: var(--header-bg); border-right: 1px solid var(--border);
    display: flex; flex-direction: column;
    position: sticky; top: 0; height: 100vh;
  }
  .sidebar-brand {
    display: flex; align-items: center; gap: 0.6rem;
    padding: 1rem 1rem 0.75rem;
    font-size: 1rem; font-weight: 600; color: var(--text);
    border-bottom: 1px solid var(--border);
  }
  .sidebar-nav { flex: 1; padding: 0.5rem; display: flex; flex-direction: column; gap: 0.1rem; }
  .nav-item {
    display: flex; align-items: center; gap: 0.6rem;
    padding: 0.45rem 0.6rem; border-radius: 4px;
    font-size: 0.875rem; color: var(--text2);
    background: none; border: none; cursor: pointer; text-decoration: none;
    text-align: left; width: 100%;
  }
  .nav-item:hover { background: var(--row-hover); color: var(--text); }
  .nav-item.active { background: var(--row-hover); color: var(--accent); }
  .sidebar-footer {
    padding: 0.75rem; border-top: 1px solid var(--border);
    display: flex; align-items: center; gap: 0.4rem;
  }
  .user-row { display: flex; align-items: center; gap: 0.4rem; flex: 1; min-width: 0; color: var(--text2); font-size: 0.8rem; }
  .username { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .icon-btn { background: none; border: none; color: var(--text2); cursor: pointer; padding: 0.3rem; border-radius: 3px; display: flex; }
  .icon-btn:hover { background: var(--border); color: var(--text); }

  /* Main */
  .main { flex: 1; display: flex; flex-direction: column; min-width: 0; padding: 1rem 1.25rem; }

  /* Toolbar */
  .toolbar {
    display: flex; align-items: center; justify-content: space-between;
    gap: 0.75rem; margin-bottom: 0.75rem; flex-wrap: wrap;
  }
  .toolbar-right { display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
  .search-wrap { position: relative; }
  .search-icon { position: absolute; left: 0.5rem; top: 50%; transform: translateY(-50%); color: var(--text2); display: flex; pointer-events: none; }
  .search {
    background: var(--input-bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.4rem 0.75rem 0.4rem 1.75rem;
    border-radius: 4px; font-size: 0.85rem; width: 200px;
  }
  .search:focus { outline: none; border-color: var(--accent); }
  .actions { display: flex; gap: 0.4rem; flex-wrap: wrap; }
  .btn {
    display: flex; align-items: center; gap: 0.35rem;
    background: var(--surface); border: 1px solid var(--border);
    color: var(--text); padding: 0.35rem 0.7rem;
    border-radius: 4px; cursor: pointer; font-size: 0.82rem;
  }
  .btn:hover { background: var(--row-hover); }
  .btn.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
  .btn.primary:hover { background: var(--accent-h); border-color: var(--accent-h); }
  .btn.danger { border-color: var(--danger); color: var(--danger); }
  .btn.danger:hover { background: var(--danger-bg); }

  /* New Folder Bar */
  .new-folder-bar {
    display: flex; align-items: center; gap: 0.5rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: 4px; padding: 0.5rem 0.75rem;
    margin-bottom: 0.75rem; color: var(--text2);
  }
  .new-folder-bar input {
    flex: 1; background: none; border: none; color: var(--text);
    font-size: 0.875rem; outline: none;
  }

  /* Table */
  .table-wrap { border: 1px solid var(--border); border-radius: 4px; overflow: hidden; }
  .table-meta {
    padding: 0.4rem 0.75rem; font-size: 0.78rem; color: var(--text2);
    background: var(--surface); border-bottom: 1px solid var(--border);
    display: flex; justify-content: space-between;
  }
  .shortcuts { color: var(--text2); }
  table { width: 100%; border-collapse: collapse; }
  thead tr { background: var(--surface); }
  th {
    padding: 0.5rem 0.75rem; text-align: left;
    font-size: 0.75rem; color: var(--text2);
    text-transform: uppercase; letter-spacing: 0.05em;
    border-bottom: 1px solid var(--border);
    white-space: nowrap; user-select: none;
  }
  th.sortable { cursor: pointer; display: flex; align-items: center; gap: 0.3rem; }
  th.sortable:hover { color: var(--text); }
  th.col-check { width: 2rem; }
  .empty {
    padding: 3rem; text-align: center; color: var(--text2);
    display: flex; flex-direction: column; align-items: center; gap: 0.75rem;
  }
  .empty p { font-size: 0.875rem; }

  @media (max-width: 640px) {
    .sidebar { display: none; }
    .hide-mobile { display: none; }
    .main { padding: 0.75rem; }
    .search { width: 140px; }
  }
</style>
