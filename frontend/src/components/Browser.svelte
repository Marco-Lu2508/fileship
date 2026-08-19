<script>
  import { onMount, tick } from 'svelte'
  import { files, loading, currentPath, selected, loadFiles, mkdir,
           deleteFile, saveScrollPosition, getScrollPosition } from '../stores/files.js'
  import { user, logout, apiFetch } from '../stores/auth.js'
  import { connectWS, disconnectWS } from '../stores/ws.js'
  import { success, error } from '../stores/toast.js'
  import { favorites, loadFavorites, addFavorite, removeFavorite } from '../stores/favorites.js'
  import { sources, activeSourceId, loadSources } from '../stores/sources.js'
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
  import Trash       from './Trash.svelte'

  // State
  let showActionPanel = false
  let showNewFolder   = false
  let newFolderName   = ''
  let showNewFile     = false
  let newFileName     = ''
  let sortKey         = 'name'
  let sortAsc         = true
  let searchQuery     = ''
  let searchResults   = null
  let searchLoading   = false
  let previewFile     = null
  let shareFile       = null
  let editorFile      = null
  let showSettings    = false
  let showThemePicker = false
  let showTrash       = false
  let searchTimeout   = null
  let dragOverPath    = null
  let viewMode        = localStorage.getItem('fileship_view') || 'list'
  let tableWrap       = null
  let searchInput     = null
  let indexing        = false

  onMount(() => {
    loadFiles('')
    loadFavorites()
    loadSources()
    connectWS()
    return disconnectWS
  })

  // Scroll-Position wiederherstellen
  $: if (!$loading && tableWrap && $currentPath !== undefined) {
    const saved = getScrollPosition($currentPath)
    if (saved > 0) {
      tick().then(() => { if (tableWrap) tableWrap.scrollTop = saved })
    }
  }

  function onTableScroll() {
    if (tableWrap) saveScrollPosition($currentPath, tableWrap.scrollTop)
  }

  function navigateTo(path) {
    if (tableWrap) saveScrollPosition($currentPath, tableWrap.scrollTop)
    showActionPanel = false
    loadFiles(path)
  }

  function onKeydown(e) {
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return
    if (e.ctrlKey || e.metaKey) {
      if (e.key === 'n') { e.preventDefault(); openNewFolder() }
      if (e.key === 'a') { e.preventDefault(); selected.set(new Set(sorted.map(f => f.path))) }
      if (e.key === 'k' || e.key === 'f') { e.preventDefault(); searchInput?.focus() }
    }
    if (e.key === 'Delete' && get(selected).size > 0) deleteSelected()
    if (e.key === 'Escape') {
      selected.set(new Set())
      searchQuery = ''
      searchResults = null
      showNewFolder = false
      showNewFile = false
      showActionPanel = false
    }
    if (e.key === 'r' && !e.ctrlKey && !e.metaKey) loadFiles(get(currentPath))
  }

  function openNewFolder() {
    showNewFolder = true
    showNewFile = false
    showActionPanel = false
    tick().then(() => document.getElementById('new-folder-input')?.focus())
  }

  function openNewFile() {
    showNewFile = true
    showNewFolder = false
    showActionPanel = false
    tick().then(() => document.getElementById('new-file-input')?.focus())
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

  async function handleTouch() {
    const name = newFileName.trim()
    if (!name) return
    const path = get(currentPath)
    const fullPath = path ? `${path}/${name}` : name
    const res = await apiFetch('/api/files/touch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: fullPath })
    })
    if (res.ok) { success(`Created "${name}"`); await loadFiles(path) }
    else error('Could not create file')
    newFileName = ''
    showNewFile = false
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
    a.href = url; a.download = 'fileship-download.zip'; a.click()
    URL.revokeObjectURL(url)
  }

  async function triggerReindex() {
    indexing = true
    const res = await apiFetch('/api/files/reindex', { method: 'POST' })
    if (res.ok) success('Indexing started – search results will improve shortly')
    else error('Reindex failed')
    setTimeout(() => indexing = false, 2000)
  }

  async function handleDragOver(e, targetPath) {
    if (e) e.preventDefault()
    dragOverPath = targetPath
  }

  async function handleDrop(e, targetPath) {
    if (e) e.preventDefault()
    dragOverPath = null
    const srcPath = e.dataTransfer.getData('fileship/path')
    if (!srcPath || srcPath === targetPath) return
    const name = srcPath.split('/').pop()
    const dst  = targetPath ? targetPath + '/' + name : name
    const res  = await apiFetch('/api/files/move', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ src: srcPath, dst })
    })
    if (res.ok) { await loadFiles(get(currentPath)); success('Moved') }
    else error('Move failed')
  }

  function setSort(key) {
    sortAsc = sortKey === key ? !sortAsc : true
    sortKey = key
  }

  function setView(mode) {
    viewMode = mode
    localStorage.setItem('fileship_view', mode)
  }

  function onSearch() {
    clearTimeout(searchTimeout)
    searchResults = null
    if (!searchQuery.trim()) { searchLoading = false; return }
    searchLoading = true
    searchTimeout = setTimeout(async () => {
      const res = await apiFetch(`/api/files/search?q=${encodeURIComponent(searchQuery)}`)
      if (res.ok) searchResults = await res.json()
      searchLoading = false
    }, 280)
  }

  $: isSearching = searchQuery.trim().length > 0
  $: localMatches = ($files?.files || []).filter(f =>
    f.name.toLowerCase().includes(searchQuery.trim().toLowerCase()))
  $: displayFiles = searchResults !== null
    ? searchResults
    : (isSearching ? localMatches : ($files?.files || []))
  $: sorted = [...displayFiles].sort((a, b) => {
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
    let av = a[sortKey], bv = b[sortKey]
    if (typeof av === 'string') av = av.toLowerCase()
    if (typeof bv === 'string') bv = bv.toLowerCase()
    return sortAsc ? (av > bv ? 1 : -1) : (av < bv ? 1 : -1)
  })
  $: totalFiles  = $files?.total ?? 0
  $: selCount    = $selected.size
  $: hasSelected = selCount > 0
</script>

<svelte:window onkeydown={onKeydown} />

<!-- Action-Panel Backdrop -->
{#if showActionPanel}
  <div class="panel-backdrop" role="button" tabindex="-1"
    onclick={() => showActionPanel = false}
    onkeydown={(e) => e.key === 'Escape' && (showActionPanel = false)}
    aria-label="Close action panel"></div>
{/if}

<!-- Slide-out Action Panel -->
<div class="action-panel" class:open={showActionPanel} aria-label="Actions panel">
  <div class="panel-header">
    <span class="panel-title">Actions</span>
    <button class="icon-btn" onclick={() => showActionPanel = false}>
      <Icon name="x" size={14} />
    </button>
  </div>
  <div class="panel-body">
    <p class="panel-section-label">Create</p>
    <button class="panel-item" onclick={openNewFolder}>
      <span class="panel-item-icon folder"><Icon name="folder" size={15} /></span>
      <span>New Folder</span>
      <span class="shortcut">Ctrl+N</span>
    </button>
    <button class="panel-item" onclick={openNewFile}>
      <span class="panel-item-icon"><Icon name="file" size={15} /></span>
      <span>New File</span>
    </button>

    {#if hasSelected}
      <div class="panel-divider"></div>
      <p class="panel-section-label">Selection ({selCount})</p>
      <button class="panel-item" onclick={downloadSelected}>
        <span class="panel-item-icon"><Icon name="download" size={15} /></span>
        <span>Download as ZIP</span>
      </button>
      <button class="panel-item danger" onclick={deleteSelected}>
        <span class="panel-item-icon danger"><Icon name="trash" size={15} /></span>
        <span>Delete selected</span>
      </button>
    {/if}

    <div class="panel-divider"></div>
    <p class="panel-section-label">View</p>
    <div class="panel-view-toggle">
      <button class="view-btn" class:active={viewMode === 'list'} onclick={() => { setView('list'); showActionPanel = false }}>
        <Icon name="list" size={14} /> List
      </button>
      <button class="view-btn" class:active={viewMode === 'grid'} onclick={() => { setView('grid'); showActionPanel = false }}>
        <Icon name="grid" size={14} /> Grid
      </button>
    </div>

    <div class="panel-divider"></div>
    <p class="panel-section-label">Index</p>
    <button class="panel-item" onclick={triggerReindex} disabled={indexing}>
      <span class="panel-item-icon" class:spinning={indexing}><Icon name="refresh" size={15} /></span>
      <span>{indexing ? 'Indexing…' : 'Rebuild search index'}</span>
    </button>
  </div>
</div>

<div class="layout">
  <!-- Sidebar -->
  <aside class="sidebar">
    <div class="sidebar-brand">
      <div class="brand-mark">
        <Icon name="folder" size={17} />
      </div>
      <div class="brand-copy">
        <strong>Fileship</strong>
      </div>
    </div>

    <nav class="sidebar-nav">
      <button class="nav-item" class:active={!showSettings} onclick={() => navigateTo(get(currentPath))}>
        <Icon name="folder" size={15} />
        <span>Files</span>
      </button>

      <!-- Multiple Sources -->
      {#if $sources.length > 1}
        <div class="nav-separator"><span>Sources</span></div>
        {#each $sources as src (src.id)}
          <button class="nav-item" class:active={$activeSourceId === src.id}
            onclick={() => { activeSourceId.set(src.id); navigateTo('') }}>
            <Icon name="folder" size={14} />
            <span>{src.name}</span>
            {#if src.is_default}<span class="source-default">default</span>{/if}
          </button>
        {/each}
      {/if}

      {#if $user?.is_admin}
        <a class="nav-item" href="/admin">
          <Icon name="users" size={15} />
          <span>Admin</span>
        </a>
      {/if}

      <!-- Favorites -->
      {#if $favorites.length > 0}
        <div class="nav-separator">
          <span>Favorites</span>
        </div>
        {#each $favorites as fav (fav.id)}
          <div class="nav-item fav-item" role="button" tabindex="0"
            onclick={() => navigateTo(fav.path)}
            onkeydown={(e) => e.key === 'Enter' && navigateTo(fav.path)}
            title={fav.path}>
            <Icon name={fav.is_dir ? 'folder' : 'file'} size={14} />
            <span class="nav-fav-name">{fav.name}</span>
            <button class="fav-remove" onclick={(e) => { e.stopPropagation(); removeFavorite(fav.path) }} title="Remove favorite">
              <Icon name="x" size={10} />
            </button>
          </div>
        {/each}
      {/if}
    </nav>

    <div class="sidebar-bottom">
      <button class="nav-item" onclick={() => showSettings = true}>
        <Icon name="settings" size={15} />
        <span>Settings</span>
      </button>
      <button class="nav-item" onclick={() => showTrash = true}>
        <Icon name="trash" size={15} />
        <span>Trash</span>
      </button>
      <button class="nav-item" onclick={() => showThemePicker = !showThemePicker}>
        <Icon name="palette" size={15} />
        <span>Theme</span>
      </button>
    </div>

    <div class="sidebar-user">
      <div class="user-avatar">
        {($user?.username?.[0] ?? '?').toUpperCase()}
      </div>
      <div class="user-info">
        <span class="user-name">{$user?.username}</span>
        {#if $user?.is_admin}
          <span class="user-badge">Admin</span>
        {/if}
      </div>
      <button class="icon-btn sm" onclick={logout} title="Sign out">
        <Icon name="logout" size={13} />
      </button>
    </div>
  </aside>

  <!-- Main content -->
  <div class="main">
    <!-- Top bar: Action toggle + Search + View -->
    <header class="topbar">
      <button class="action-toggle" onclick={() => showActionPanel = !showActionPanel}
        class:active={showActionPanel} aria-label="Toggle action panel" title="Actions">
        <Icon name="plus" size={16} />
        <span>Actions</span>
        <Icon name={showActionPanel ? 'chevron_up' : 'chevron_dn'} size={11} />
      </button>

      <div class="search-center">
        <span class="search-icon"><Icon name="search" size={14} /></span>
        <input
          bind:this={searchInput}
          class="search-input"
          placeholder="Search files… (Ctrl+K)"
          bind:value={searchQuery}
          oninput={onSearch}
          aria-label="Search files"
        />
        {#if isSearching}
          <button class="search-clear" onclick={() => { searchQuery = ''; searchResults = null }}
            title="Clear search">
            <Icon name="x" size={12} />
          </button>
        {/if}
        {#if searchLoading}
          <span class="search-spinner"></span>
        {/if}
      </div>

      <div class="topbar-right">
        {#if hasSelected}
          <div class="selection-badge">
            {selCount} selected
          </div>
          <button class="btn sm" onclick={downloadSelected} title="Download as ZIP">
            <Icon name="download" size={13} />
            <span class="hide-sm">ZIP</span>
          </button>
          <button class="btn sm danger" onclick={deleteSelected}>
            <Icon name="trash" size={13} />
            <span class="hide-sm">Delete</span>
          </button>
        {/if}
        <div class="view-toggle" aria-label="View mode">
          <button class="icon-btn" class:active={viewMode === 'list'} onclick={() => setView('list')} title="List view">
            <Icon name="list" size={14} />
          </button>
          <button class="icon-btn" class:active={viewMode === 'grid'} onclick={() => setView('grid')} title="Grid view">
            <Icon name="grid" size={14} />
          </button>
        </div>
      </div>
    </header>

    <!-- Location bar -->
    <div class="location-bar">
      <Breadcrumb path={$currentPath} onNavigate={navigateTo} />
      <span class="item-count">
        {#if searchResults !== null}
          {sorted.length} result{sorted.length !== 1 ? 's' : ''} for "<strong>{searchQuery}</strong>"
        {:else}
          {totalFiles} item{totalFiles !== 1 ? 's' : ''}
        {/if}
      </span>
    </div>

    <!-- Inline create bars -->
    {#if showNewFolder}
      <div class="create-bar" role="form" aria-label="Create folder">
        <span class="create-icon folder"><Icon name="folder" size={14} /></span>
        <input
          id="new-folder-input"
          placeholder="Folder name"
          bind:value={newFolderName}
          onkeydown={(e) => { if (e.key === 'Enter') handleMkdir(); if (e.key === 'Escape') { showNewFolder = false; newFolderName = '' } }}
        />
        <button class="btn primary sm" onclick={handleMkdir}>Create</button>
        <button class="btn sm" onclick={() => { showNewFolder = false; newFolderName = '' }}>Cancel</button>
      </div>
    {/if}
    {#if showNewFile}
      <div class="create-bar" role="form" aria-label="Create file">
        <span class="create-icon"><Icon name="file" size={14} /></span>
        <input
          id="new-file-input"
          placeholder="File name (e.g. notes.md)"
          bind:value={newFileName}
          onkeydown={(e) => { if (e.key === 'Enter') handleTouch(); if (e.key === 'Escape') { showNewFile = false; newFileName = '' } }}
        />
        <button class="btn primary sm" onclick={handleTouch}>Create</button>
        <button class="btn sm" onclick={() => { showNewFile = false; newFileName = '' }}>Cancel</button>
      </div>
    {/if}

    <Dropzone />

    <!-- File list -->
    <div class="file-wrap" class:grid-view={viewMode === 'grid'} bind:this={tableWrap} onscroll={onTableScroll}>
      {#if $loading || searchLoading}
        <Skeleton rows={14} />
      {:else if sorted.length === 0}
        <div class="empty-state">
          <div class="empty-icon">
            <Icon name={isSearching ? 'search' : 'folder'} size={30} />
          </div>
          <p class="empty-title">
            {isSearching ? `No results for "${searchQuery}"` : 'This folder is empty'}
          </p>
          {#if !isSearching}
            <p class="empty-sub">Drop files here or use Actions to create</p>
          {/if}
        </div>
      {:else}
        <table>
          <thead>
            <tr>
              <th class="col-check">
                <input
                  type="checkbox"
                  onchange={(e) => selected.set(e.target.checked ? new Set(sorted.map(f => f.path)) : new Set())}
                  checked={selCount === sorted.length && sorted.length > 0}
                  aria-label="Select all"
                />
              </th>
              <th class="col-name sortable" onclick={() => setSort('name')}>
                Name {#if sortKey === 'name'}<Icon name={sortAsc ? 'sort_up' : 'sort_dn'} size={11} />{/if}
              </th>
              <th class="col-size sortable" onclick={() => setSort('size')}>
                Size {#if sortKey === 'size'}<Icon name={sortAsc ? 'sort_up' : 'sort_dn'} size={11} />{/if}
              </th>
              <th class="col-date sortable hide-mobile" onclick={() => setSort('mod_time')}>
                Modified {#if sortKey === 'mod_time'}<Icon name={sortAsc ? 'sort_up' : 'sort_dn'} size={11} />{/if}
              </th>
              <th class="col-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each sorted as file, i (file.path)}
              <FileRow
                {file}
                {dragOverPath}
                rowIndex={i}
                onPreview={(f) => previewFile = f}
                onShare={(f) => shareFile = f}
                onEdit={(f) => editorFile = f}
                onNavigate={navigateTo}
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

<!-- Modals & overlays -->
<Preview     file={previewFile}  onClose={() => previewFile = null} />
<ShareDialog file={shareFile}    onClose={() => shareFile = null} />
<Editor      file={editorFile}   onClose={() => editorFile = null} />
{#if showSettings}    <Settings    onClose={() => showSettings = false} /> {/if}
{#if showThemePicker} <ThemePicker onClose={() => showThemePicker = false} /> {/if}
{#if showTrash}       <Trash       onClose={() => showTrash = false} /> {/if}
<Toast />

<style>
  /* ── Layout ─────────────────────────────────────── */
  .layout {
    display: flex;
    min-height: 100vh;
    background: var(--bg);
    color: var(--text);
  }

  /* ── Sidebar ────────────────────────────────────── */
  .sidebar {
    width: 220px;
    flex-shrink: 0;
    background: var(--header-bg);
    border-right: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    position: sticky;
    top: 0;
    height: 100vh;
    overflow: hidden;
  }

  .sidebar-brand {
    display: flex;
    align-items: center;
    gap: 0.65rem;
    padding: 1rem 1.1rem;
    border-bottom: 1px solid var(--border);
  }

  .brand-mark {
    width: 30px;
    height: 30px;
    border-radius: 7px;
    background: var(--accent);
    display: grid;
    place-items: center;
    color: #fff;
    flex-shrink: 0;
  }

  .brand-copy strong {
    font-size: 0.95rem;
    font-weight: 700;
    color: var(--text);
    letter-spacing: -0.01em;
  }

  .sidebar-nav {
    flex: 1;
    padding: 0.6rem 0.6rem 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow-y: auto;
  }

  .sidebar-bottom {
    padding: 0 0.6rem 0.4rem;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .nav-item {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.6rem 0.7rem;
    border-radius: var(--radius);
    font-size: 0.85rem;
    color: var(--text2);
    background: none;
    border: none;
    cursor: pointer;
    text-decoration: none;
    text-align: left;
    width: 100%;
    transition: background var(--transition), color var(--transition);
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

  .nav-separator {
    padding: 0.6rem 0.7rem 0.2rem;
    font-size: 0.65rem;
    font-weight: 700;
    color: var(--text3);
    text-transform: uppercase;
    letter-spacing: 0.1em;
  }

  .fav-item { padding-right: 0.3rem; }

  .nav-fav-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .fav-remove {
    background: none; border: none; color: var(--text3);
    cursor: pointer; display: flex; padding: 0.15rem;
    border-radius: 3px; flex-shrink: 0; opacity: 0;
    transition: opacity var(--transition), background var(--transition);
  }

  .fav-item:hover .fav-remove { opacity: 1; }
  .fav-remove:hover { background: var(--danger-bg); color: var(--danger); }

  .source-default {
    margin-left: auto;
    font-size: 0.6rem; font-weight: 700;
    background: var(--accent-soft); color: var(--accent);
    padding: 0.1rem 0.35rem; border-radius: 20px;
    text-transform: uppercase; letter-spacing: 0.05em;
  }

  .sidebar-user {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.75rem 0.85rem;
    border-top: 1px solid var(--border);
  }

  .user-avatar {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background: var(--accent);
    color: #fff;
    display: grid;
    place-items: center;
    font-size: 0.7rem;
    font-weight: 700;
    flex-shrink: 0;
  }

  .user-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .user-name {
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .user-badge {
    font-size: 0.65rem;
    color: var(--accent);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  /* ── Action Panel ───────────────────────────────── */
  .panel-backdrop {
    position: fixed;
    inset: 0;
    z-index: 200;
    background: transparent;
  }

  .action-panel {
    position: fixed;
    top: 0;
    left: 220px;
    height: 100vh;
    width: 260px;
    background: var(--surface);
    border-right: 1px solid var(--border);
    z-index: 210;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    transform: translateX(-100%);
    transition: transform 0.2s cubic-bezier(.2,.8,.3,1);
    overflow: hidden;
  }

  .action-panel.open {
    transform: translateX(0);
  }

  .panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.9rem 1rem;
    border-bottom: 1px solid var(--border);
  }

  .panel-title {
    font-size: 0.8rem;
    font-weight: 700;
    color: var(--text);
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .panel-body {
    flex: 1;
    overflow-y: auto;
    padding: 0.75rem;
  }

  .panel-section-label {
    font-size: 0.68rem;
    font-weight: 700;
    color: var(--text3);
    text-transform: uppercase;
    letter-spacing: 0.1em;
    padding: 0.25rem 0.5rem 0.4rem;
  }

  .panel-item {
    display: flex;
    align-items: center;
    gap: 0.65rem;
    width: 100%;
    padding: 0.6rem 0.7rem;
    background: none;
    border: none;
    color: var(--text2);
    font-size: 0.85rem;
    cursor: pointer;
    border-radius: var(--radius);
    text-align: left;
    transition: background var(--transition), color var(--transition);
  }

  .panel-item:hover:not(:disabled) {
    background: var(--row-hover);
    color: var(--text);
  }

  .panel-item:disabled { opacity: 0.5; cursor: not-allowed; }

  .panel-item.danger:hover { background: var(--danger-bg); color: var(--danger); }

  .panel-item-icon {
    width: 26px;
    height: 26px;
    display: grid;
    place-items: center;
    border-radius: 6px;
    background: var(--surface2);
    color: var(--text2);
    flex-shrink: 0;
    transition: background var(--transition);
  }

  .panel-item-icon.folder { color: var(--accent); background: var(--accent-soft); }
  .panel-item-icon.danger { color: var(--danger); background: var(--danger-bg); }
  .panel-item-icon.spinning { animation: spin 1s linear infinite; }

  .shortcut {
    margin-left: auto;
    font-size: 0.68rem;
    color: var(--text3);
    font-family: monospace;
  }

  .panel-divider {
    height: 1px;
    background: var(--border);
    margin: 0.5rem 0;
  }

  .panel-view-toggle {
    display: flex;
    gap: 0.4rem;
    padding: 0 0.5rem;
  }

  .view-btn {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    padding: 0.5rem;
    background: var(--surface2);
    border: 1px solid var(--border);
    color: var(--text2);
    font-size: 0.82rem;
    cursor: pointer;
    border-radius: var(--radius);
    transition: all var(--transition);
  }

  .view-btn.active {
    background: var(--accent-soft);
    border-color: var(--accent);
    color: var(--accent);
    font-weight: 600;
  }

  /* ── Main area ──────────────────────────────────── */
  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  /* ── Topbar ─────────────────────────────────────── */
  .topbar {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1.25rem;
    border-bottom: 1px solid var(--border);
    background: var(--header-bg);
    position: sticky;
    top: 0;
    z-index: 100;
  }

  .action-toggle {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.48rem 0.8rem;
    background: var(--surface);
    border: 1px solid var(--border);
    color: var(--text2);
    font-size: 0.82rem;
    font-weight: 600;
    cursor: pointer;
    border-radius: var(--radius);
    white-space: nowrap;
    transition: all var(--transition);
    flex-shrink: 0;
  }

  .action-toggle:hover, .action-toggle.active {
    background: var(--accent-soft);
    border-color: var(--accent);
    color: var(--accent);
  }

  /* ── Central search ─────────────────────────────── */
  .search-center {
    flex: 1;
    position: relative;
    display: flex;
    align-items: center;
    max-width: 520px;
    margin: 0 auto;
  }

  .search-icon {
    position: absolute;
    left: 0.75rem;
    color: var(--text3);
    display: flex;
    pointer-events: none;
    z-index: 1;
  }

  .search-input {
    width: 100%;
    padding: 0.55rem 2.8rem 0.55rem 2.2rem;
    background: var(--input-bg);
    border: 1px solid var(--border);
    border-radius: 20px;
    color: var(--text);
    font-size: 0.875rem;
    transition: border-color var(--transition), box-shadow var(--transition);
  }

  .search-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  .search-input::placeholder { color: var(--text3); }

  .search-clear {
    position: absolute;
    right: 0.65rem;
    background: none;
    border: none;
    color: var(--text3);
    cursor: pointer;
    display: flex;
    padding: 0.2rem;
    border-radius: 50%;
  }

  .search-clear:hover { color: var(--text); background: var(--border); }

  .search-spinner {
    position: absolute;
    right: 0.7rem;
    width: 14px;
    height: 14px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
    pointer-events: none;
  }

  /* ── Topbar right ───────────────────────────────── */
  .topbar-right {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex-shrink: 0;
  }

  .selection-badge {
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 0.75rem;
    font-weight: 700;
    padding: 0.25rem 0.6rem;
    border-radius: 20px;
    white-space: nowrap;
    border: 1px solid rgba(79,158,255,0.25);
  }

  .view-toggle {
    display: flex;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }

  /* ── Location bar ───────────────────────────────── */
  .location-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.55rem 1.25rem;
    border-bottom: 1px solid var(--border2);
    background: var(--surface2);
    min-height: 36px;
  }

  .item-count {
    font-size: 0.75rem;
    color: var(--text3);
    white-space: nowrap;
    margin-left: 0.75rem;
    flex-shrink: 0;
  }

  .item-count strong { color: var(--text2); }

  /* ── Inline create bars ─────────────────────────── */
  .create-bar {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.6rem 1rem;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    animation: bar-in 0.15s ease both;
  }

  @keyframes bar-in {
    from { opacity: 0; transform: translateY(-6px); }
    to   { opacity: 1; transform: none; }
  }

  .create-icon {
    display: flex;
    color: var(--text3);
    flex-shrink: 0;
  }

  .create-icon.folder { color: var(--accent); }

  .create-bar input {
    flex: 1;
    background: var(--input-bg);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.45rem 0.65rem;
    border-radius: var(--radius);
    font-size: 0.875rem;
    min-width: 0;
  }

  .create-bar input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  /* ── File table wrapper ─────────────────────────── */
  .file-wrap {
    flex: 1;
    overflow-y: auto;
    overflow-x: auto;
    max-height: calc(100vh - 116px);
    background: var(--surface);
  }

  table {
    width: 100%;
    border-collapse: collapse;
  }

  thead {
    position: sticky;
    top: 0;
    z-index: 10;
    background: var(--surface2);
  }

  th {
    padding: 0.6rem 1rem;
    text-align: left;
    font-size: 0.7rem;
    color: var(--text3);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
    user-select: none;
  }

  th.sortable {
    cursor: pointer;
    transition: color var(--transition);
  }

  th.sortable:hover { color: var(--text2); }
  th.col-check { width: 2.5rem; }
  th.col-actions { width: 11rem; }

  /* ── Empty state ────────────────────────────────── */
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 6rem 2rem;
    gap: 0.75rem;
    animation: fade-in 0.25s ease;
  }

  @keyframes fade-in {
    from { opacity: 0; transform: translateY(8px); }
    to   { opacity: 1; transform: none; }
  }

  .empty-icon {
    width: 60px;
    height: 60px;
    border-radius: 50%;
    background: var(--surface2);
    display: grid;
    place-items: center;
    color: var(--text3);
    border: 1px solid var(--border);
  }

  .empty-title {
    font-size: 0.95rem;
    font-weight: 600;
    color: var(--text2);
  }

  .empty-sub {
    font-size: 0.82rem;
    color: var(--text3);
  }

  /* ── Buttons ────────────────────────────────────── */
  .btn {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    background: var(--surface);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.5rem 0.85rem;
    border-radius: var(--radius);
    cursor: pointer;
    font-size: 0.82rem;
    font-weight: 500;
    transition: background var(--transition), border-color var(--transition);
    white-space: nowrap;
  }

  .btn:hover { background: var(--row-hover); }
  .btn.sm { padding: 0.4rem 0.65rem; font-size: 0.78rem; }
  .btn.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
  .btn.primary:hover { background: var(--accent-h); border-color: var(--accent-h); }
  .btn.danger { color: var(--danger); border-color: var(--danger); background: transparent; }
  .btn.danger:hover { background: var(--danger-bg); }

  .icon-btn {
    background: none;
    border: none;
    color: var(--text2);
    cursor: pointer;
    padding: 0.35rem;
    border-radius: var(--radius);
    display: grid;
    place-items: center;
    transition: background var(--transition), color var(--transition);
  }

  .icon-btn:hover { background: var(--row-hover); color: var(--text); }
  .icon-btn.active { background: var(--accent-soft); color: var(--accent); }
  .icon-btn.sm { padding: 0.25rem; }

  /* ── Grid view ──────────────────────────────────── */
  :global(.grid-view table) { display: block; }
  :global(.grid-view thead) { display: none; }
  :global(.grid-view tbody) {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(175px, 1fr));
    gap: 0.65rem;
    padding: 1rem;
    background: var(--bg);
  }
  :global(.grid-view tr) {
    display: flex;
    flex-direction: column;
    padding: 0.9rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--surface);
    cursor: pointer;
    transition: border-color var(--transition), box-shadow var(--transition), transform var(--transition);
    animation: grid-item-in 0.2s ease both;
  }
  :global(.grid-view tr:hover) {
    border-color: var(--accent);
    box-shadow: var(--shadow);
    transform: translateY(-1px);
  }
  :global(.grid-view td) { padding: 0; border: none; }
  :global(.grid-view .col-check) { order: 5; margin-top: auto; padding-top: 0.5rem; border-top: 1px solid var(--border2); }
  :global(.grid-view .col-name) { min-height: 3.5rem; align-items: flex-start !important; padding: 0.4rem 0; }
  :global(.grid-view .file-icon-wrap) { width: 38px; height: 38px; }
  :global(.grid-view .col-size) { padding-top: 0.2rem; }
  :global(.grid-view .col-date) { display: none; }
  :global(.grid-view .col-actions) { padding-top: 0.4rem; flex-wrap: wrap; gap: 0.15rem; }

  @keyframes grid-item-in {
    from { opacity: 0; transform: scale(0.95); }
    to   { opacity: 1; transform: none; }
  }

  /* ── Animations ─────────────────────────────────── */
  @keyframes spin { to { transform: rotate(360deg); } }

  /* ── Responsive ─────────────────────────────────── */
  @media (max-width: 680px) {
    .sidebar { display: none; }
    .action-panel { left: 0; }
    .hide-mobile { display: none !important; }
    .topbar { padding: 0.6rem 0.75rem; }
    .location-bar { padding: 0.45rem 0.75rem; }
    .search-input { font-size: 0.8rem; }
  }

  @media (max-width: 480px) {
    .hide-sm { display: none; }
    .search-center { max-width: 100%; }
  }
</style>
