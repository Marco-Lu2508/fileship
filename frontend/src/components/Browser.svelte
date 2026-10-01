<script>
  import { onMount, tick } from 'svelte'
  import { get } from 'svelte/store'
  import {
    files,
    loading,
    loadError,
    currentPath,
    selected,
    loadFiles,
    mkdir,
    deleteFile,
    saveScrollPosition,
    getScrollPosition
  } from '../stores/files.js'
  import { user, logout, apiFetch } from '../stores/auth.js'
  import { connectWS, disconnectWS, connection } from '../stores/ws.js'
  import { success, error } from '../stores/toast.js'
  import { favorites, loadFavorites } from '../stores/favorites.js'
  import { fileKind, validName, requireOK, saveDownload } from '../lib/files.js'
  import Breadcrumb from './Breadcrumb.svelte'
  import Dropzone from './Dropzone.svelte'
  import FileRow from './FileRow.svelte'
  import Preview from './Preview.svelte'
  import ShareDialog from './ShareDialog.svelte'
  import Toast from './Toast.svelte'
  import Skeleton from './Skeleton.svelte'
  import Editor from './Editor.svelte'
  import Settings from './Settings.svelte'
  import ThemePicker from './ThemePicker.svelte'
  import Trash from './Trash.svelte'
  import Icon from './Icon.svelte'
  import ConfirmDialog from './ConfirmDialog.svelte'

  let showMenu = false,
    mobileNav = false,
    createKind = '',
    createName = '',
    creating = false
  let sortKey = 'name',
    sortAsc = true,
    filter = 'all'
  let searchQuery = '',
    searchResults = null,
    searchLoading = false,
    searchError = ''
  let searchTimeout,
    searchController,
    searchInput,
    tableWrap,
    uploader,
    reindexTimeout
  let previewFile = null,
    shareFile = null,
    editorFile = null
  let showSettings = false,
    showThemePicker = false,
    showTrash = false,
    indexing = false
  let dragOverPath = null,
    deletePaths = [],
    deleting = false,
    downloading = false
  let viewMode =
    localStorage.getItem('fileship_view') === 'grid' ? 'grid' : 'list'
  let viewportWidth = window.innerWidth

  onMount(() => {
    loadFiles('', { page: 1, sort_by: sortKey, sort_asc: sortAsc })
    loadFavorites().catch(() => error('Could not load favorites'))
    connectWS()
    return () => {
      disconnectWS()
      clearTimeout(searchTimeout)
      clearTimeout(reindexTimeout)
      searchController?.abort()
    }
  })

  function clearSearch() {
    clearTimeout(searchTimeout)
    searchController?.abort()
    searchQuery = ''
    searchResults = null
    searchLoading = false
    searchError = ''
  }
  async function navigateTo(path) {
    if (tableWrap) saveScrollPosition(get(currentPath), tableWrap.scrollTop)
    clearSearch()
    showMenu = false
    mobileNav = false
    filter = 'all'
    await loadFiles(path, { page: 1 })
    await tick()
    if (tableWrap) tableWrap.scrollTop = getScrollPosition(path)
  }
  function onKeydown(e) {
    if (
      previewFile ||
      shareFile ||
      editorFile ||
      showSettings ||
      showThemePicker ||
      showTrash ||
      deletePaths.length
    )
      return
    if (e.key === 'Escape') {
      if (createKind) {
        createKind = ''
        return
      }
      clearSearch()
      selected.set(new Set())
      showMenu = false
      mobileNav = false
    }
    if (e.target.closest('input, textarea, select, [contenteditable="true"]'))
      return
    if (e.ctrlKey || e.metaKey) {
      if (e.key.toLowerCase() === 'n') {
        e.preventDefault()
        openCreate('folder')
      }
      if (e.key.toLowerCase() === 'a') {
        e.preventDefault()
        selected.set(new Set(sorted.map((f) => f.path)))
      }
      if (e.key.toLowerCase() === 'k') {
        e.preventDefault()
        searchInput?.focus()
      }
      return
    }
    if (e.key === 'Delete') requestDelete([...get(selected)])
    if (e.key === 'r') loadFiles(get(currentPath))
  }
  async function openCreate(kind) {
    createKind = kind
    createName = ''
    showMenu = false
    await tick()
    document.getElementById('create-name')?.focus()
  }
  async function createItem(e) {
    e.preventDefault()
    if (creating) return
    const name = createName.trim()
    if (!validName(name)) {
      error('Use a name without slashes or control characters')
      return
    }
    creating = true
    try {
      const parent = get(currentPath),
        path = parent ? `${parent}/${name}` : name
      if (createKind === 'folder') await mkdir(path)
      else
        await requireOK(
          await apiFetch('/api/files/touch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path })
          })
        )
      success(`Created “${name}”`)
      createKind = ''
      createName = ''
      await loadFiles(parent)
    } catch (err) {
      error(err.message)
    } finally {
      creating = false
    }
  }
  function requestDelete(paths) {
    if (paths.length) {
      deletePaths = paths
      showMenu = false
    }
  }
  async function confirmDelete() {
    deleting = true
    const results = await Promise.allSettled(
      deletePaths.map((path) => deleteFile(path))
    )
    const failed = deletePaths.filter(
        (_, index) => results[index].status === 'rejected'
      ),
      count = deletePaths.length - failed.length
    deletePaths = []
    deleting = false
    if (count) success(`Moved ${count} item${count === 1 ? '' : 's'} to trash`)
    if (failed.length)
      error(
        `${failed.length} item(s) failed: ${results.find((r) => r.status === 'rejected').reason.message}`
      )
    await loadFiles(get(currentPath))
    selected.set(new Set(failed))
    await loadFavorites().catch(() => {})
  }
  async function downloadSelected() {
    if (downloading) return
    downloading = true
    try {
      await saveDownload(
        await apiFetch('/api/files/zip-multi', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ paths: [...get(selected)] })
        }),
        'fileship-download.zip'
      )
    } catch (err) {
      error(err.message)
    } finally {
      downloading = false
    }
  }
  async function triggerReindex() {
    indexing = true
    showMenu = false
    try {
      await requireOK(await apiFetch('/api/files/reindex', { method: 'POST' }))
      success('Search indexing started')
    } catch (err) {
      error(err.message)
    }
    reindexTimeout = setTimeout(() => (indexing = false), 2000)
  }
  async function moveFile(e, targetPath) {
    e.preventDefault()
    dragOverPath = null
    const src = e.dataTransfer?.getData('fileship/path')
    if (!src || src === targetPath || targetPath.startsWith(src + '/')) return
    const name = src.split('/').pop()
    try {
      await requireOK(
        await apiFetch('/api/files/move', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            src,
            dst: targetPath ? `${targetPath}/${name}` : name
          })
        })
      )
      success(`Moved “${name}”`)
      await loadFiles(get(currentPath))
    } catch (err) {
      error(err.message)
    }
  }
  async function unzip(file) {
    try {
      await requireOK(
        await apiFetch('/api/files/unzip', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            zip_path: file.path,
            dest_dir: file.path.replace(/\.zip$/i, '')
          })
        })
      )
      success('Archive extracted')
      await loadFiles(get(currentPath))
    } catch (err) {
      error(err.message)
    }
  }
  function setSort(key, ascending = sortKey === key ? !sortAsc : true) {
    sortKey = key
    sortAsc = ascending
    loadFiles(get(currentPath), { page: 1, sort_by: key, sort_asc: ascending })
  }
  function setView(mode) {
    viewMode = mode
    localStorage.setItem('fileship_view', mode)
  }
  function setFilter(value) {
    filter = value
    selected.set(new Set())
    mobileNav = false
  }
  function onSearch() {
    clearTimeout(searchTimeout)
    searchController?.abort()
    searchResults = null
    searchError = ''
    selected.set(new Set())
    const query = searchQuery.trim()
    searchLoading = !!query
    if (!query) return
    const controller = new AbortController()
    searchController = controller
    searchTimeout = setTimeout(async () => {
      try {
        const response = await requireOK(
          await apiFetch(`/api/files/search?q=${encodeURIComponent(query)}`, {
            signal: controller.signal
          }),
          'Search failed'
        )
        const results = await response.json()
        if (!controller.signal.aborted) searchResults = results || []
      } catch (err) {
        if (!controller.signal.aborted) searchError = err.message
      } finally {
        if (!controller.signal.aborted) searchLoading = false
      }
    }, 280)
  }
  const filters = [
    { id: 'all', label: 'All files' },
    { id: 'folder', label: 'Folders' },
    { id: 'text', label: 'Documents' },
    { id: 'image', label: 'Images' },
    { id: 'video', label: 'Video' },
    { id: 'audio', label: 'Audio' }
  ]
  $: favoritePaths = new Set($favorites.map((f) => f.path))
  $: isSearching = !!searchQuery.trim()
  $: displayFiles = (searchResults ?? $files.files ?? []).map((file) => ({
    ...file,
    starred: favoritePaths.has(file.path)
  }))
  $: filtered = displayFiles.filter(
    (file) =>
      filter === 'all' ||
      (filter === 'starred' ? file.starred : fileKind(file) === filter)
  )
  // Preserve server ordering across folder pages; sort recursive search locally.
  $: sorted = isSearching
    ? [...filtered].sort((a, b) => {
        if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
        const c =
          sortKey === 'name'
            ? a.name.localeCompare(b.name, undefined, {
                numeric: true,
                sensitivity: 'base'
              })
            : sortKey === 'mod_time'
              ? new Date(a.mod_time) - new Date(b.mod_time)
              : a.size - b.size
        return sortAsc ? c : -c
      })
    : filtered
  $: if (viewportWidth > 800) mobileNav = false
  $: totalPages = Math.max(1, Math.ceil($files.total / $files.per_page))
  $: selCount = $selected.size
  $: title = isSearching
    ? 'Search results'
    : $currentPath
      ? $currentPath.split('/').pop()
      : 'Your files'
  $: visibleError = searchError || $loadError
</script>

<svelte:window onkeydown={onKeydown} bind:innerWidth={viewportWidth} />
<div class="layout">
  {#if mobileNav}<button
      class="nav-backdrop"
      aria-label="Close navigation"
      onclick={() => (mobileNav = false)}
    ></button>{/if}
  <aside
    class="sidebar"
    class:mobile-open={mobileNav}
    aria-label="Workspace navigation"
    inert={viewportWidth <= 800 && !mobileNav}
  >
    <button
      class="drawer-close"
      aria-label="Close navigation"
      onclick={() => (mobileNav = false)}><Icon name="x" size={18} /></button
    >
    <a class="brand" href="/" aria-label="Fileship home"
      ><Icon name="ship" size={29} /><strong>fileship<span>.</span></strong></a
    >
    <div class="workspace-label">
      <i></i>Personal workspace<span>SELF-HOSTED</span>
    </div>
    <nav class="sidebar-nav" aria-label="Main navigation">
      <span class="nav-label">LIBRARY</span>
      <button
        class="nav-item"
        class:active={filter !== 'starred'}
        onclick={() => navigateTo('')}
        ><Icon name="folder" size={19} /><span>All files</span></button
      >
      <button
        class="nav-item"
        class:active={filter === 'starred'}
        onclick={() => setFilter('starred')}
        ><Icon name="star" size={19} /><span>Favorites in this folder</span
        ></button
      >
      <button
        class="nav-item"
        onclick={() => {
          showTrash = true
          mobileNav = false
        }}><Icon name="trash" size={19} /><span>Trash</span></button
      >
      <span class="nav-label favorites-label">QUICK ACCESS</span>
      {#each $favorites.slice(0, 8) as fav (fav.id)}<button
          class="nav-item favorite-link"
          title={fav.path}
          onclick={() => {
            if (fav.is_dir) navigateTo(fav.path)
            else {
              previewFile = { ...fav, mime_type: '', size: 0 }
              mobileNav = false
            }
          }}
          ><Icon name={fav.is_dir ? 'folder' : 'file'} size={16} /><span
            >{fav.name}</span
          ></button
        >{:else}<p class="favorites-hint">
          Star a file or folder to keep it close.
        </p>{/each}
    </nav>
    <div class="sidebar-note">
      <Icon name="lock" size={17} />
      <div>
        <strong>Your files. Your space.</strong>
        <p>Stored on your own server.</p>
      </div>
    </div>
    <div class="sidebar-bottom">
      {#if $user?.is_admin}<a class="nav-item" href="/admin"
          ><Icon name="users" size={18} /><span>Administration</span></a
        >{/if}<button
        class="nav-item"
        onclick={() => {
          showSettings = true
          mobileNav = false
        }}><Icon name="settings" size={18} /><span>Settings</span></button
      ><button
        class="nav-item"
        onclick={() => {
          showThemePicker = true
          mobileNav = false
        }}><Icon name="palette" size={18} /><span>Appearance</span></button
      >
    </div>
    <div class="sidebar-user">
      <span class="avatar">{($user?.username?.[0] ?? '?').toUpperCase()}</span>
      <div>
        <strong>{$user?.username}</strong><span
          >{$user?.is_admin ? 'Administrator' : 'Member'}</span
        >
      </div>
      <button onclick={logout} aria-label="Sign out" title="Sign out"
        ><Icon name="logout" size={17} /></button
      >
    </div>
  </aside>
  <main class="main" inert={mobileNav && viewportWidth <= 800}>
    <header class="topbar">
      <button
        class="icon-btn mobile-menu"
        aria-label="Open navigation"
        aria-expanded={mobileNav}
        onclick={() => (mobileNav = !mobileNav)}
        ><Icon name="menu" size={21} /></button
      ><Breadcrumb path={$currentPath} onNavigate={navigateTo} />
      <div class="search-box">
        <Icon name="search" size={17} /><input
          bind:this={searchInput}
          bind:value={searchQuery}
          oninput={onSearch}
          placeholder="Search your files…"
          aria-label="Search files"
        />{#if isSearching}<button
            class="icon-btn"
            onclick={clearSearch}
            aria-label="Clear search"><Icon name="x" size={15} /></button
          >{:else}<kbd>Ctrl K</kbd>{/if}
      </div>
      <span
        class="connection"
        class:offline={$connection !== 'connected'}
        title={$connection === 'connected'
          ? 'Live updates connected'
          : 'Live updates reconnecting'}
        ><i></i>{$connection === 'connected' ? 'Live' : 'Connecting'}</span
      >
    </header>
    <div class="workspace">
      <section class="page-heading">
        <div>
          <span class="eyebrow">A LITTLE ORDER. A LOT OF POSSIBILITY.</span>
          <h1>{title}<span>.</span></h1>
          <p>
            {isSearching
              ? 'Find what you need, anywhere in your workspace.'
              : 'Everything you need, right where you left it.'}
          </p>
        </div>
        <div class="heading-actions">
          <button class="btn" onclick={() => openCreate('folder')}
            ><Icon name="plus" size={17} />New folder</button
          ><button class="btn primary" onclick={() => uploader?.openPicker()}
            ><Icon name="upload" size={17} />Upload files</button
          >
        </div>
      </section>
      <Dropzone bind:this={uploader} />
      <section class="file-section" aria-label="File browser">
        <div class="file-toolbar">
          <div class="filter-tabs" aria-label="Filter current page">
            {#each filters as item}<button
                class:active={filter === item.id}
                aria-pressed={filter === item.id}
                onclick={() => setFilter(item.id)}>{item.label}</button
              >{/each}{#if filter === 'starred'}<button
                class="active"
                onclick={() => setFilter('all')}
                >Favorites <Icon name="x" size={12} /></button
              >{/if}
          </div>
          <div class="toolbar-actions">
            <button
              class="icon-btn"
              aria-label="Refresh folder"
              title="Refresh folder"
              disabled={$loading}
              onclick={() => loadFiles($currentPath)}
              ><Icon name="refresh" size={17} /></button
            >
            <div class="view-toggle">
              <button
                class:active={viewMode === 'list'}
                aria-label="List view"
                aria-pressed={viewMode === 'list'}
                onclick={() => setView('list')}
                ><Icon name="list" size={17} /></button
              ><button
                class:active={viewMode === 'grid'}
                aria-label="Grid view"
                aria-pressed={viewMode === 'grid'}
                onclick={() => setView('grid')}
                ><Icon name="grid" size={17} /></button
              >
            </div>
          </div>
        </div>
        <div class="list-meta">
          <span
            >{isSearching
              ? `${searchResults?.length ?? 0} results${searchResults?.length >= 200 ? ' (first 200)' : ''}`
              : `${$files.total} items`}<span class="meta-divider">/</span
            >{filter === 'all'
              ? 'All types'
              : filter === 'starred'
                ? 'Favorites on this page'
                : `${filters.find((f) => f.id === filter)?.label} on this page`}</span
          >
          <div class="list-controls">
            <label
              >Sort by <select
                aria-label="Sort files"
                value={`${sortKey}:${sortAsc}`}
                onchange={(e) => {
                  const [key, asc] = e.target.value.split(':')
                  setSort(key, asc === 'true')
                }}
                ><option value="name:true">Name A–Z</option><option
                  value="name:false">Name Z–A</option
                ><option value="mod_time:false">Newest first</option><option
                  value="mod_time:true">Oldest first</option
                ><option value="size:false">Largest first</option><option
                  value="size:true">Smallest first</option
                ></select
              ></label
            >
            <div class="menu-wrap">
              <button
                class="icon-btn"
                aria-label="More actions"
                aria-expanded={showMenu}
                onclick={() => (showMenu = !showMenu)}
                ><Icon name="more" size={19} /></button
              >{#if showMenu}<button
                  class="menu-backdrop"
                  aria-label="Close actions"
                  onclick={() => (showMenu = false)}
                ></button>
                <div class="action-menu">
                  <button onclick={() => openCreate('file')}
                    ><Icon name="file" size={16} />New file</button
                  ><button
                    onclick={() => {
                      uploader?.openFolderPicker()
                      showMenu = false
                    }}><Icon name="folder" size={16} />Upload folder</button
                  ><button onclick={triggerReindex} disabled={indexing}
                    ><Icon name="refresh" size={16} />{indexing
                      ? 'Indexing…'
                      : 'Rebuild search index'}</button
                  >
                </div>{/if}
            </div>
          </div>
        </div>
        {#if selCount}<div class="selection-bar" role="status">
            <span><strong>{selCount}</strong> selected</span><button
              onclick={downloadSelected}
              disabled={downloading}
              ><Icon name="download" size={16} />{downloading
                ? 'Preparing…'
                : 'Download ZIP'}</button
            ><button
              class="danger"
              onclick={() => requestDelete([...$selected])}
              ><Icon name="trash" size={16} />Move to trash</button
            ><button
              class="clear-selection"
              aria-label="Clear selection"
              onclick={() => selected.set(new Set())}
              ><Icon name="x" size={16} /></button
            >
          </div>{/if}
        {#if createKind}<form class="create-bar" onsubmit={createItem}>
            <Icon
              name={createKind === 'folder' ? 'folder' : 'file'}
              size={19}
            /><input
              id="create-name"
              bind:value={createName}
              placeholder={createKind === 'folder'
                ? 'Name your folder'
                : 'Name your file'}
              aria-label={createKind === 'folder' ? 'Folder name' : 'File name'}
              required
            /><button class="btn primary" disabled={creating}
              >{creating ? 'Creating…' : 'Create'}</button
            ><button
              type="button"
              class="icon-btn"
              aria-label="Cancel create"
              onclick={() => (createKind = '')}
              ><Icon name="x" size={18} /></button
            >
          </form>{/if}
        <div
          class="file-wrap"
          class:grid-view={viewMode === 'grid'}
          bind:this={tableWrap}
          aria-busy={$loading || searchLoading}
        >
          {#if $loading || searchLoading}<Skeleton rows={8} />
          {:else if visibleError}<div class="empty-state" role="alert">
              <span class="empty-icon"><Icon name="warning" size={30} /></span>
              <h2>Something didn’t load</h2>
              <p>{visibleError}</p>
              <button
                class="btn"
                onclick={() =>
                  isSearching ? onSearch() : loadFiles($currentPath)}
                >Try again</button
              >
            </div>
          {:else if sorted.length === 0}<div class="empty-state">
              <span class="empty-icon"
                ><Icon
                  name={isSearching
                    ? 'search'
                    : filter === 'starred'
                      ? 'star'
                      : 'folder'}
                  size={34}
                /></span
              >
              <h2>
                {isSearching
                  ? 'No files found'
                  : filter !== 'all'
                    ? 'Nothing here yet'
                    : 'Make room for your next idea'}
              </h2>
              <p>
                {isSearching
                  ? `No matches for “${searchQuery}”. Try another name.`
                  : filter !== 'all'
                    ? 'Try another filter or a different page.'
                    : 'Upload your first file or create a folder to get started.'}
              </p>
              {#if isSearching}<button class="btn" onclick={clearSearch}
                  >Clear search</button
                >{:else if filter !== 'all'}<button
                  class="btn"
                  onclick={() => setFilter('all')}>Show all files</button
                >{:else}<button
                  class="btn primary"
                  onclick={() => uploader?.openPicker()}
                  ><Icon name="upload" size={17} />Upload files</button
                >{/if}
            </div>
          {:else}<table>
              <thead
                ><tr
                  ><th class="col-check"
                    ><input
                      type="checkbox"
                      aria-label="Select all visible files"
                      checked={selCount > 0 &&
                        sorted.every((f) => $selected.has(f.path))}
                      indeterminate={selCount > 0 &&
                        !sorted.every((f) => $selected.has(f.path))}
                      onchange={(e) =>
                        selected.set(
                          e.target.checked
                            ? new Set(sorted.map((f) => f.path))
                            : new Set()
                        )}
                    /></th
                  ><th
                    aria-sort={sortKey === 'name'
                      ? sortAsc
                        ? 'ascending'
                        : 'descending'
                      : 'none'}
                    ><button onclick={() => setSort('name')}
                      >Name {#if sortKey === 'name'}<Icon
                          name={sortAsc ? 'sort_up' : 'sort_dn'}
                          size={12}
                        />{/if}</button
                    ></th
                  ><th
                    class="col-size"
                    aria-sort={sortKey === 'size'
                      ? sortAsc
                        ? 'ascending'
                        : 'descending'
                      : 'none'}
                    ><button onclick={() => setSort('size')}>Size</button></th
                  ><th
                    class="col-date"
                    aria-sort={sortKey === 'mod_time'
                      ? sortAsc
                        ? 'ascending'
                        : 'descending'
                      : 'none'}
                    ><button onclick={() => setSort('mod_time')}
                      >Last modified</button
                    ></th
                  ><th class="col-actions"
                    ><span class="sr-only">Actions</span></th
                  ></tr
                ></thead
              ><tbody
                >{#each sorted as file (file.path)}<FileRow
                    showPath={isSearching}
                    {file}
                    {dragOverPath}
                    onPreview={(f) => (previewFile = f)}
                    onShare={(f) => (shareFile = f)}
                    onEdit={(f) => (editorFile = f)}
                    onNavigate={navigateTo}
                    onUnzip={unzip}
                    onDelete={(f) => requestDelete([f.path])}
                    onDragOver={(e, path) => {
                      e.preventDefault()
                      dragOverPath = path
                    }}
                    onDrop={moveFile}
                  />{/each}</tbody
              >
            </table>{/if}
        </div>
        <footer class="file-footer">
          <span
            >{sorted.length} shown{!isSearching
              ? ` of ${$files.total} items`
              : ''}</span
          >{#if !isSearching && totalPages > 1}<div class="pagination">
              <button
                class="icon-btn"
                aria-label="Previous page"
                disabled={$files.page <= 1 || $loading}
                onclick={() =>
                  loadFiles($currentPath, { page: $files.page - 1 })}
                ><Icon name="chevron_l" size={16} /></button
              ><span>Page {$files.page} of {totalPages}</span><button
                class="icon-btn"
                aria-label="Next page"
                disabled={$files.page >= totalPages || $loading}
                onclick={() =>
                  loadFiles($currentPath, { page: $files.page + 1 })}
                ><Icon name="chevron_r" size={16} /></button
              >
            </div>{:else}<span class="keyboard-hint"
              ><kbd>Ctrl K</kbd> to search <span>·</span> Drag files to a folder</span
            >{/if}
        </footer>
      </section>
      <p class="workspace-footer">
        <Icon name="ship" size={15} />A home for everything you’re working on.
      </p>
    </div>
  </main>
</div>
<Preview file={previewFile} onClose={() => (previewFile = null)} /><ShareDialog
  file={shareFile}
  onClose={() => (shareFile = null)}
/><Editor file={editorFile} onClose={() => (editorFile = null)} />
{#if showSettings}<Settings
    onClose={() => (showSettings = false)}
  />{/if}{#if showThemePicker}<ThemePicker
    onClose={() => (showThemePicker = false)}
  />{/if}{#if showTrash}<Trash
    onClose={() => {
      showTrash = false
      loadFiles($currentPath)
    }}
  />{/if}
{#if deletePaths.length}<ConfirmDialog
    title={`Move ${deletePaths.length === 1 ? 'this item' : `${deletePaths.length} items`} to trash?`}
    description="You can restore these items from Trash for 30 days."
    confirmLabel="Move to trash"
    busy={deleting}
    onConfirm={confirmDelete}
    onClose={() => {
      if (!deleting) deletePaths = []
    }}
  />{/if}<Toast />

<style>
  .drawer-close {
    display: none;
    position: absolute;
    right: 12px;
    top: 16px;
    background: none;
    border: 0;
    color: #c5d1c8;
    padding: 8px;
    cursor: pointer;
  }
  @media (max-width: 800px) {
    .drawer-close {
      display: grid;
    }
  }
  .layout {
    display: flex;
    min-height: 100dvh;
  }
  .sidebar {
    width: 252px;
    flex-shrink: 0;
    height: 100dvh;
    position: sticky;
    top: 0;
    display: flex;
    flex-direction: column;
    padding: 30px 18px 0;
    background: var(--sidebar-bg);
    color: #eef3ef;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 11px;
    color: #b9d99e;
    padding: 0 12px;
    text-decoration: none;
  }
  .brand strong {
    font-size: 29px;
    letter-spacing: -1.4px;
    color: #f6faf7;
  }
  .brand strong span {
    color: #b9d99e;
  }
  .workspace-label {
    margin: 27px 12px 30px;
    color: #c0cec4;
    font-size: 11px;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  .workspace-label i {
    width: 5px;
    height: 5px;
    background: #b9d99e;
    border-radius: 50%;
  }
  .workspace-label span {
    font-size: 8px;
    letter-spacing: 1px;
    padding: 3px 5px;
    border: 1px solid #415647;
    border-radius: 4px;
    margin-left: 13px;
  }
  .sidebar-nav {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }
  .nav-label {
    display: block;
    color: #94a79b;
    font-size: 9px;
    letter-spacing: 1.5px;
    padding: 0 14px 12px;
    font-weight: 600;
  }
  .favorites-label {
    padding-top: 35px;
  }
  .nav-item {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 14px;
    margin: 3px 0;
    border: 0;
    background: transparent;
    border-radius: 8px;
    color: #c5d1c8;
    text-align: left;
    cursor: pointer;
    font-size: 13px;
    text-decoration: none;
  }
  .nav-item span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .nav-item:hover {
    background: #2e4435;
    color: white;
  }
  .nav-item.active {
    background: #b9d99e;
    color: #22382b;
    font-weight: 600;
  }
  .favorite-link {
    font-size: 12px;
    padding-top: 10px;
    padding-bottom: 10px;
  }
  .favorites-hint {
    color: #9aac9f;
    font-size: 12px;
    line-height: 1.8;
    margin: 0 14px;
    max-width: 170px;
  }
  .sidebar-note {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 25px 7px 15px;
    padding: 14px 10px;
    border: 1px solid #3f5446;
    border-radius: 9px;
    color: #b9d99e;
  }
  .sidebar-note strong {
    font-weight: 500;
    font-size: 11px;
    color: #edf3ed;
  }
  .sidebar-note p {
    color: #a1b2a5;
    font-size: 10px;
    margin-top: 4px;
  }
  .sidebar-bottom {
    padding-bottom: 15px;
  }
  .sidebar-bottom .nav-item {
    padding: 9px 14px;
    font-size: 12px;
  }
  .sidebar-user {
    display: flex;
    align-items: center;
    gap: 10px;
    border-top: 1px solid #3b5143;
    margin: 0 -18px;
    padding: 18px 24px;
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    background: #d6dec7;
    color: #304133;
    border-radius: 50%;
    font-size: 13px;
    font-weight: 600;
    flex-shrink: 0;
  }
  .sidebar-user div {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }
  .sidebar-user strong {
    font-size: 12px;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .sidebar-user div span {
    font-size: 10px;
    color: #a5b8a9;
  }
  .sidebar-user button {
    margin-left: auto;
    background: none;
    border: 0;
    color: #b5c7b9;
    cursor: pointer;
    padding: 8px;
    border-radius: 5px;
  }
  .sidebar-user button:hover {
    background: #3a5141;
  }
  .main {
    flex: 1;
    min-width: 0;
  }
  .topbar {
    display: flex;
    align-items: center;
    gap: 22px;
    padding: 20px 40px;
    border-bottom: 1px solid var(--border);
    min-height: 81px;
    background: var(--surface);
  }
  .search-box {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 12px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface2);
    width: 330px;
    margin-left: auto;
    color: var(--text3);
  }
  .search-box:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .search-box input {
    width: 100%;
    min-width: 0;
    outline: 0;
    border: 0;
    background: none;
    color: var(--text);
    font-size: 12px;
  }
  .search-box kbd {
    white-space: nowrap;
    border: 1px solid var(--border);
    padding: 1px 5px;
    border-radius: 4px;
    font-size: 9px;
  }
  .connection {
    display: flex;
    align-items: center;
    gap: 7px;
    color: var(--text2);
    font-size: 11px;
  }
  .connection i {
    background: var(--success);
    height: 6px;
    width: 6px;
    border-radius: 50%;
  }
  .connection.offline i {
    background: var(--warning);
  }
  .workspace {
    max-width: 1600px;
    margin: 0 auto;
    padding: 41px 40px 20px;
  }
  .page-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    margin-bottom: 29px;
  }
  .eyebrow {
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 1.6px;
    color: var(--text3);
  }
  h1 {
    font-size: clamp(28px, 3vw, 38px);
    line-height: 1.3;
    letter-spacing: -1.6px;
    font-weight: 600;
    margin: 7px 0 9px;
    overflow-wrap: anywhere;
  }
  h1 span {
    color: var(--accent);
  }
  .page-heading p {
    color: var(--text2);
    font-size: 12px;
  }
  .heading-actions {
    display: flex;
    gap: 9px;
    flex-shrink: 0;
  }
  .btn {
    display: inline-flex;
    justify-content: center;
    align-items: center;
    gap: 8px;
    padding: 11px 16px;
    border: 1px solid var(--border);
    border-radius: 7px;
    background: var(--surface);
    color: var(--text);
    cursor: pointer;
    font-weight: 500;
    font-size: 12px;
    white-space: nowrap;
  }
  .btn:hover {
    background: var(--row-hover);
    border-color: var(--text3);
  }
  .btn.primary {
    background: var(--accent);
    color: var(--on-accent);
    border-color: var(--accent);
  }
  .btn.primary:hover {
    background: var(--accent-h);
  }
  .icon-btn {
    display: grid;
    place-items: center;
    padding: 7px;
    color: var(--text2);
    border: 0;
    background: none;
    border-radius: 5px;
    cursor: pointer;
  }
  .icon-btn:hover {
    background: var(--row-hover);
    color: var(--text);
  }
  button:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .file-section {
    margin-top: 29px;
  }
  .file-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    border-bottom: 1px solid var(--border);
  }
  .filter-tabs {
    display: flex;
    align-items: center;
    gap: 24px;
    overflow-x: auto;
  }
  .filter-tabs button {
    border: 0;
    border-bottom: 2px solid transparent;
    background: none;
    padding: 0 0 15px;
    color: var(--text2);
    font-size: 12px;
    cursor: pointer;
    white-space: nowrap;
    display: flex;
    gap: 7px;
    align-items: center;
  }
  .filter-tabs button.active {
    color: var(--accent);
    border-bottom-color: var(--accent);
    font-weight: 600;
  }
  .filter-tabs button:hover {
    color: var(--text);
  }
  .toolbar-actions {
    display: flex;
    align-items: center;
    gap: 14px;
    padding-bottom: 10px;
  }
  .view-toggle {
    display: flex;
    padding: 3px;
    border: 1px solid var(--border);
    background: var(--surface);
    border-radius: 6px;
  }
  .view-toggle button {
    display: grid;
    place-items: center;
    border: 0;
    background: none;
    padding: 5px 7px;
    color: var(--text3);
    cursor: pointer;
    border-radius: 3px;
  }
  .view-toggle button.active {
    color: var(--text);
    background: var(--surface3);
  }
  .list-meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 18px 0;
    gap: 10px;
    font-size: 11px;
    color: var(--text3);
  }
  .meta-divider {
    margin: 0 12px;
    color: var(--border2);
  }
  .list-controls {
    display: flex;
    gap: 15px;
    align-items: center;
  }
  .list-controls label {
    display: flex;
    align-items: center;
    gap: 7px;
  }
  .list-controls select {
    border: 0;
    color: var(--text2);
    background: var(--bg);
    padding: 4px;
    font-size: 11px;
    cursor: pointer;
  }
  .menu-wrap {
    position: relative;
  }
  .menu-backdrop {
    position: fixed;
    inset: 0;
    opacity: 0;
    z-index: 20;
    cursor: default;
  }
  .action-menu {
    position: absolute;
    right: 0;
    top: 35px;
    z-index: 21;
    width: 205px;
    padding: 6px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 9px;
    box-shadow: var(--shadow-lg);
  }
  .action-menu button {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 11px;
    background: none;
    border: 0;
    border-radius: 5px;
    color: var(--text);
    font-size: 12px;
    cursor: pointer;
    text-align: left;
  }
  .action-menu button:hover {
    background: var(--row-hover);
  }
  .selection-bar {
    display: flex;
    align-items: center;
    gap: 19px;
    background: var(--accent-soft);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 15px;
    margin-bottom: 12px;
    font-size: 12px;
    color: var(--accent);
  }
  .selection-bar button {
    display: flex;
    align-items: center;
    gap: 7px;
    background: none;
    border: 0;
    color: var(--accent);
    cursor: pointer;
    font-size: 11px;
  }
  .selection-bar .danger {
    color: var(--danger);
  }
  .clear-selection {
    margin-left: auto;
  }
  .create-bar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    margin-bottom: 12px;
  }
  .create-bar input {
    flex: 1;
    min-width: 0;
    padding: 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text);
    background: var(--input-bg);
  }
  .file-wrap {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    overflow: auto;
    max-height: 60vh;
  }
  table {
    width: 100%;
    border-collapse: collapse;
  }
  thead {
    background: var(--surface2);
    position: sticky;
    top: 0;
    z-index: 2;
  }
  th {
    text-align: left;
    padding: 13px 17px;
    border-bottom: 1px solid var(--border);
    font-weight: 500;
    font-size: 10px;
    color: var(--text3);
    white-space: nowrap;
  }
  th button {
    display: flex;
    align-items: center;
    gap: 9px;
    background: none;
    border: 0;
    color: inherit;
    font-size: inherit;
    cursor: pointer;
  }
  th.col-check {
    width: 48px;
  }
  th.col-size {
    width: 110px;
  }
  th.col-date {
    width: 165px;
  }
  th.col-actions {
    width: 130px;
  }
  .empty-state {
    display: flex;
    align-items: center;
    flex-direction: column;
    text-align: center;
    gap: 12px;
    padding: 65px 20px;
  }
  .empty-icon {
    display: grid;
    place-items: center;
    width: 72px;
    height: 72px;
    background: var(--accent-soft);
    color: var(--accent);
    border-radius: 20px;
    margin-bottom: 8px;
  }
  .empty-state h2 {
    font-size: 19px;
    font-weight: 500;
    letter-spacing: -0.4px;
  }
  .empty-state p {
    color: var(--text2);
    max-width: 370px;
    font-size: 12px;
  }
  .empty-state .btn {
    margin-top: 7px;
  }
  .file-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 15px;
    padding: 14px 0;
    font-size: 10px;
    color: var(--text3);
  }
  .pagination {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .keyboard-hint {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .keyboard-hint kbd {
    font-size: 9px;
    background: var(--surface);
    padding: 3px 6px;
    border: 1px solid var(--border);
    border-radius: 4px;
  }
  .workspace-footer {
    margin: 27px 0 10px;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    font-size: 10px;
    color: var(--text3);
  }
  :global(.grid-view table) {
    display: block;
  }
  :global(.grid-view thead) {
    display: none;
  }
  :global(.grid-view tbody) {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(195px, 1fr));
    gap: 15px;
    padding: 17px;
  }
  :global(.grid-view tr) {
    display: flex;
    flex-direction: column;
    position: relative;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 9px;
    padding: 15px;
  }
  :global(.grid-view tr:hover) {
    border-color: var(--accent);
  }
  :global(.grid-view td) {
    padding: 0;
    border: 0;
  }
  :global(.grid-view .col-check) {
    position: absolute;
    top: 15px;
    right: 15px;
    width: auto;
  }
  :global(.grid-view .col-name) {
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
    padding-bottom: 10px;
  }
  :global(.grid-view .file-icon-wrap) {
    width: 48px;
    height: 48px;
    border-radius: 11px;
  }
  :global(.grid-view .file-icon-wrap svg) {
    width: 24px;
    height: 24px;
  }
  :global(.grid-view .col-date) {
    display: none;
  }
  :global(.grid-view .col-actions) {
    margin-top: 14px;
    padding-top: 9px;
    border-top: 1px solid var(--border);
  }
  :global(.grid-view .name-text) {
    max-width: 100%;
  }
  :global(.grid-view .name-btn) {
    width: 100%;
  }
  .mobile-menu {
    display: none;
  }
  @media (max-width: 1100px) {
    .sidebar {
      width: 222px;
    }
    .workspace {
      padding: 28px 24px 18px;
    }
    .topbar {
      padding: 18px 24px;
    }
    .filter-tabs {
      gap: 18px;
    }
    .page-heading {
      align-items: flex-start;
    }
    .heading-actions {
      flex-direction: column-reverse;
    }
    .search-box {
      width: 270px;
    }
  }
  @media (max-width: 800px) {
    .sidebar {
      position: fixed;
      left: 0;
      top: 0;
      z-index: 50;
      width: 252px;
      transform: translateX(-100%);
      transition: transform 0.2s;
    }
    .sidebar.mobile-open {
      transform: none;
    }
    .nav-backdrop {
      position: fixed;
      inset: 0;
      z-index: 49;
      background: #0d1c1880;
      border: 0;
    }
    .mobile-menu {
      display: grid;
    }
    .topbar {
      gap: 10px;
      padding: 14px 18px;
      flex-wrap: wrap;
    }
    .connection {
      margin-left: auto;
    }
    .search-box {
      order: 4;
      width: 100%;
      margin-top: 5px;
    }
    .workspace {
      padding: 27px 18px 15px;
    }
    .page-heading {
      flex-direction: column;
      gap: 19px;
    }
    .heading-actions {
      flex-direction: row;
    }
    .filter-tabs {
      gap: 19px;
    }
    .toolbar-actions {
      gap: 6px;
    }
    .list-meta {
      font-size: 10px;
    }
    .list-controls {
      gap: 5px;
    }
    .keyboard-hint {
      display: none;
    }
    .file-wrap {
      max-height: 65vh;
    }
    .col-date {
      display: none;
    }
    :global(.file-wrap td.col-date) {
      display: none;
    }
  }
  @media (max-width: 480px) {
    .eyebrow {
      font-size: 8px;
      letter-spacing: 1.2px;
    }
    .selection-bar {
      gap: 12px;
      padding: 10px;
      flex-wrap: wrap;
    }
    .filter-tabs {
      gap: 16px;
    }
    .filter-tabs button {
      font-size: 11px;
    }
    .list-controls label {
      font-size: 0;
    }
    .list-controls select {
      max-width: 125px;
    }
    .meta-divider {
      margin: 0 7px;
    }
    .col-size {
      display: none;
    }
    :global(.file-wrap td.col-size) {
      display: none;
    }
    th {
      padding: 12px;
    }
    :global(.grid-view td.col-size) {
      display: block;
    }
    :global(.grid-view tbody) {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      padding: 10px;
      gap: 10px;
    }
    :global(.grid-view tr) {
      padding: 12px;
    }
  }
</style>
