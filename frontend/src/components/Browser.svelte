<script>
  import { onMount } from 'svelte'
  import { files, loading, currentPath, selected, loadFiles, mkdir, deleteFile } from '../stores/files.js'
  import { user, logout, apiFetch } from '../stores/auth.js'
  import { connectWS, disconnectWS } from '../stores/ws.js'
  import { success, error } from '../stores/toast.js'
  import { theme } from '../stores/theme.js'
  import { get } from 'svelte/store'
  import Breadcrumb from './Breadcrumb.svelte'
  import Dropzone from './Dropzone.svelte'
  import FileRow from './FileRow.svelte'
  import Preview from './Preview.svelte'
  import ShareDialog from './ShareDialog.svelte'
  import Toast from './Toast.svelte'
  import Skeleton from './Skeleton.svelte'
  import Editor from './Editor.svelte'
  import Settings from './Settings.svelte'

  let showNewFolder = false
  let newFolderName = ''
  let sortKey = 'name'
  let sortAsc = true
  let searchQuery = ''
  let searchResults = null
  let searchLoading = false
  let previewFile = null
  let shareFile = null
  let editorFile = null
  let showSettings = false
  let searchTimeout = null
  let dragOverPath = null   // für Drag&Drop zwischen Ordnern

  onMount(() => {
    loadFiles('')
    connectWS()
    return disconnectWS
  })

  // --- Keyboard Shortcuts ---
  function onKeydown(e) {
    // Kein Shortcut wenn in einem Input
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return

    switch(e.key) {
      case 'n':
        if (e.ctrlKey || e.metaKey) { e.preventDefault(); showNewFolder = true }
        break
      case 'Delete':
      case 'Backspace':
        if (get(selected).size > 0) deleteSelected()
        break
      case 'Escape':
        selected.set(new Set())
        searchQuery = ''
        searchResults = null
        showNewFolder = false
        break
      case 'a':
        if (e.ctrlKey || e.metaKey) {
          e.preventDefault()
          selected.set(new Set(displayFiles.map(f => f.path)))
        }
        break
      case 'r':
        if (e.key === 'r' && !e.ctrlKey) loadFiles(get(currentPath))
        break
    }
  }

  async function handleMkdir() {
    if (!newFolderName.trim()) return
    try {
      const path = get(currentPath)
      await mkdir(path ? path + '/' + newFolderName : newFolderName)
      await loadFiles(path)
      success(`Folder "${newFolderName}" created`)
    } catch {
      error('Could not create folder')
    }
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
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`
      },
      body: JSON.stringify({ paths })
    })
    if (!res.ok) { error('Download failed'); return }
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'fileship-download.zip'
    a.click()
    URL.revokeObjectURL(url)
  }

  // --- Drag & Drop zwischen Ordnern ---
  async function handleDragOver(e, targetPath) {
    e.preventDefault()
    dragOverPath = targetPath
  }

  async function handleDrop(e, targetPath) {
    e.preventDefault()
    dragOverPath = null
    const srcPath = e.dataTransfer.getData('fileship/path')
    if (!srcPath || srcPath === targetPath) return
    const name = srcPath.split('/').pop()
    const dst = targetPath ? targetPath + '/' + name : name
    const res = await apiFetch('/api/files/move', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ src: srcPath, dst })
    })
    if (res.ok) {
      await loadFiles(get(currentPath))
      success(`Moved to ${targetPath || 'root'}`)
    } else {
      error('Move failed')
    }
  }

  function setSort(key) {
    if (sortKey === key) sortAsc = !sortAsc
    else { sortKey = key; sortAsc = true }
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
  $: showing = sorted.length
  $: isDark = $theme === 'dark'
</script>

<svelte:window onkeydown={onKeydown} />

<div class="browser">
  <header>
    <div class="brand">🚀 Fileship</div>
    <div class="search-wrap">
      <input
        class="search"
        placeholder="🔍 Search files… (type to search)"
        bind:value={searchQuery}
        oninput={onSearch}
      />
    </div>
    <div class="user-info">
      <button class="theme-btn" onclick={() => theme.set(isDark ? 'light' : 'dark')} title="Toggle theme">
        {isDark ? '☀️' : '🌙'}
      </button>
      <span class="username">{$user?.username}</span>
      {#if $user?.is_admin}
        <a href="/admin" class="admin-link">⚙️ Admin</a>
      {/if}
      <button onclick={() => showSettings = true}>⚙️ Settings</button>
      <button onclick={logout}>Sign out</button>
    </div>
  </header>

  <main>
    <div class="toolbar">
      <Breadcrumb path={$currentPath} />
      <div class="actions">
        {#if $selected.size > 0}
          <button class="btn" onclick={downloadSelected}>🗜️ ZIP ({$selected.size})</button>
          <button class="btn danger" onclick={deleteSelected}>🗑️ Delete ({$selected.size})</button>
        {/if}
        <button class="btn" onclick={() => showNewFolder = !showNewFolder} title="Ctrl+N">📁 New Folder</button>
      </div>
    </div>

    {#if showNewFolder}
      <div class="new-folder">
        <input
          placeholder="Folder name"
          bind:value={newFolderName}
          onkeydown={(e) => { if (e.key === 'Enter') handleMkdir(); if (e.key === 'Escape') showNewFolder = false }}
          autofocus
        />
        <button onclick={handleMkdir}>Create</button>
        <button class="cancel" onclick={() => showNewFolder = false}>Cancel</button>
      </div>
    {/if}

    <Dropzone />

    <div class="table-wrap">
      {#if $loading || searchLoading}
        <Skeleton rows={10} />
      {:else if sorted.length === 0}
        <div class="empty">
          {searchQuery ? `No results for "${searchQuery}"` : 'This folder is empty'}
        </div>
      {:else}
        <div class="table-meta">
          {#if searchResults !== null}
            <span>{showing} result(s) for "{searchQuery}"</span>
          {:else}
            <span>{showing} of {totalFiles} items</span>
          {/if}
          <span class="shortcuts">Shortcuts: Ctrl+A select all · Del delete · Esc clear · R refresh</span>
        </div>
        <table>
          <thead>
            <tr>
              <th class="check">
                <input
                  type="checkbox"
                  onchange={(e) => selected.set(e.target.checked ? new Set(sorted.map(f => f.path)) : new Set())}
                  checked={$selected.size === sorted.length && sorted.length > 0}
                />
              </th>
              <th onclick={() => setSort('name')}>Name {sortKey==='name' ? (sortAsc?'↑':'↓') : ''}</th>
              <th onclick={() => setSort('size')}>Size {sortKey==='size' ? (sortAsc?'↑':'↓') : ''}</th>
              <th class="hide-mobile" onclick={() => setSort('mod_time')}>Modified {sortKey==='mod_time' ? (sortAsc?'↑':'↓') : ''}</th>
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
  </main>
</div>

<Preview file={previewFile} onClose={() => previewFile = null} />
<ShareDialog file={shareFile} onClose={() => shareFile = null} />
<Editor file={editorFile} onClose={() => editorFile = null} />
{#if showSettings}<Settings onClose={() => showSettings = false} />{/if}
<Toast />

<style>
  .browser { min-height: 100vh; background: var(--bg); color: var(--text); display: flex; flex-direction: column; }
  header {
    display: flex; align-items: center; gap: 1rem;
    padding: 0.75rem 1.5rem; background: var(--surface);
    border-bottom: 1px solid var(--border); flex-wrap: wrap;
  }
  .brand { font-size: 1.3rem; font-weight: 700; flex-shrink: 0; }
  .search-wrap { flex: 1; min-width: 200px; }
  .search {
    width: 100%; background: var(--bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.45rem 0.9rem; border-radius: 8px; font-size: 0.9rem;
  }
  .search:focus { outline: none; border-color: var(--accent); }
  .user-info { display: flex; align-items: center; gap: 0.75rem; font-size: 0.9rem; color: var(--muted); flex-shrink: 0; }
  .username { display: none; }
  .theme-btn { background: none; border: 1px solid var(--border); padding: 0.3rem 0.5rem; border-radius: 6px; cursor: pointer; font-size: 1rem; }
  .theme-btn:hover { background: var(--border); }
  .user-info button { background: none; border: 1px solid var(--border); color: var(--muted); padding: 0.3rem 0.75rem; border-radius: 6px; cursor: pointer; }
  .user-info button:hover { background: var(--border); color: var(--text); }
  .admin-link { color: var(--accent); text-decoration: none; font-size: 0.85rem; }
  main { padding: 1.25rem 1.5rem; flex: 1; }
  .toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1rem; flex-wrap: wrap; gap: 0.5rem; }
  .actions { display: flex; gap: 0.5rem; flex-wrap: wrap; }
  .btn { background: var(--surface); border: 1px solid var(--border); color: var(--text); padding: 0.4rem 0.9rem; border-radius: 6px; cursor: pointer; font-size: 0.9rem; }
  .btn:hover { background: var(--border); }
  .btn.danger { border-color: #7f1d1d; color: var(--danger); }
  .btn.danger:hover { background: var(--danger-bg); }
  .new-folder { display: flex; gap: 0.5rem; margin-bottom: 1rem; flex-wrap: wrap; }
  .new-folder input { background: var(--surface); border: 1px solid var(--border); color: var(--text); padding: 0.4rem 0.75rem; border-radius: 6px; font-size: 0.9rem; flex: 1; min-width: 150px; }
  .new-folder input:focus { outline: none; border-color: var(--accent); }
  .new-folder button { background: var(--accent); border: none; color: #fff; padding: 0.4rem 0.9rem; border-radius: 6px; cursor: pointer; }
  .new-folder button.cancel { background: var(--border); color: var(--text); }
  .table-wrap { margin-top: 1rem; border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
  .table-meta { padding: 0.5rem 0.75rem; font-size: 0.8rem; color: var(--muted); background: var(--surface); border-bottom: 1px solid var(--border); display: flex; justify-content: space-between; flex-wrap: wrap; gap: 0.5rem; }
  .shortcuts { display: none; }
  table { width: 100%; border-collapse: collapse; }
  thead tr { background: var(--surface); }
  th { padding: 0.6rem 0.75rem; text-align: left; font-size: 0.8rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.05em; cursor: pointer; user-select: none; white-space: nowrap; }
  th:hover { color: var(--text); }
  th.check { width: 2rem; cursor: default; }
  .empty { padding: 3rem; text-align: center; color: var(--muted); }

  @media (min-width: 640px) {
    .username { display: inline; }
    .shortcuts { display: inline; }
  }
  @media (max-width: 640px) {
    .hide-mobile { display: none; }
    header { padding: 0.75rem 1rem; }
    main { padding: 1rem; }
  }
</style>
