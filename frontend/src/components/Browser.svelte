<script>
  import { locale, translator as i18n } from '../stores/i18n.js'
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
  import { fileKind, formatBytes, validName, requireOK } from '../lib/files.js'
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
  import TransferDialog from './TransferDialog.svelte'
  import Properties from './Properties.svelte'
  import Shares from './Shares.svelte'
  import Help from './Help.svelte'
  import DownloadDialog from './DownloadDialog.svelte'
  import { preferences } from '../stores/preferences.js'
  import { transferFiles } from '../lib/operations.js'

  let transfer = null,
    clipboard = null,
    infoPath = null,
    showShares = false,
    showHelp = false
  let storage = null
  let showUpload = false,
    uploadDragging = false,
    selectionAnchor = null
  let archivePaths = null
  function selectFile(file, event) {
    selected.update((current) => {
      const next = new Set(current)
      if (event?.shiftKey && selectionAnchor) {
        const start = sorted.findIndex((item) => item.path === selectionAnchor),
          end = sorted.findIndex((item) => item.path === file.path)
        if (start >= 0 && end >= 0) {
          sorted
            .slice(Math.min(start, end), Math.max(start, end) + 1)
            .forEach((item) => next.add(item.path))
          return next
        }
      }
      next.has(file.path) ? next.delete(file.path) : next.add(file.path)
      selectionAnchor = file.path
      return next
    })
  }
  function dropUpload(event) {
    uploadDragging = false
    if (event.defaultPrevented || !event.dataTransfer?.files.length) return
    event.preventDefault()
    uploader?.upload(event.dataTransfer.files)
  }
  async function loadStorage() {
    try {
      storage = await (
        await requireOK(await apiFetch('/api/me/settings'))
      ).json()
    } catch {}
  }
  function startTransfer(mode, paths = [...get(selected)]) {
    if (paths.length)
      transfer = { mode, paths: [...paths], initialPath: get(currentPath) }
  }
  function copySelection(mode) {
    const paths = [...get(selected)]
    if (paths.length) {
      clipboard = { mode, paths }
      success(
        mode === 'copy'
          ? $i18n('Items copied to clipboard')
          : $i18n('Items ready to move')
      )
    }
  }
  async function transferred(results) {
    const done = results.filter((r) => r.ok).map((r) => r.path)
    if (clipboard?.mode === 'move') {
      clipboard.paths = clipboard.paths.filter((path) => !done.includes(path))
      if (!clipboard.paths.length) clipboard = null
    }
    if (done.length) {
      success($i18n('File operation completed'))
      await loadFiles(get(currentPath))
      await loadFavorites().catch(() => {})
      loadStorage()
    }
  }

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
    deleting = false
  let viewMode = ['grid', 'gallery'].includes(
    localStorage.getItem('fileship_view')
  )
    ? localStorage.getItem('fileship_view')
    : 'list'
  let viewportWidth = window.innerWidth

  onMount(() => {
    loadFiles('', { page: 1, sort_by: sortKey, sort_asc: sortAsc })
    loadFavorites().catch(() => error($i18n('Could not load favorites')))
    connectWS(loadStorage)
    loadStorage()
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
      transfer ||
      infoPath !== null ||
      showShares ||
      showHelp ||
      archivePaths ||
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
    if (
      e.target.closest(
        'input:not([type="checkbox"]), textarea, select, [contenteditable="true"]'
      )
    )
      return
    if (e.key === 'F1') {
      e.preventDefault()
      showHelp = true
      return
    }
    if (e.key === 'F2' && get(selected).size === 1) {
      e.preventDefault()
      document
        .querySelector(
          '[data-file-path="' + CSS.escape([...get(selected)][0]) + '"]'
        )
        ?.dispatchEvent(new CustomEvent('rename-item'))
      return
    }
    if (e.altKey && e.key === 'ArrowUp') {
      e.preventDefault()
      navigateTo(get(currentPath).split('/').slice(0, -1).join('/'))
      return
    }
    if (e.ctrlKey || e.metaKey) {
      if (
        ['c', 'x', 'v'].includes(e.key.toLowerCase()) &&
        !window.getSelection()?.toString()
      ) {
        e.preventDefault()
        if (e.key.toLowerCase() === 'v') {
          if (clipboard) startTransfer(clipboard.mode, clipboard.paths)
        } else copySelection(e.key.toLowerCase() === 'c' ? 'copy' : 'move')
      }
      if (e.key.toLowerCase() === 'n') {
        e.preventDefault()
        openCreate('folder')
      }
      if (e.key.toLowerCase() === 'a') {
        e.preventDefault()
        selected.set(new Set(sorted.map((f) => f.path)))
      }
      if (['k', 'f'].includes(e.key.toLowerCase())) {
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
      error($i18n('Use a name without slashes or control characters'))
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
      success($i18n('Created “{name}”', { name }))
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
    if (count) success($i18n('Moved {count} items to trash', { count }))
    if (failed.length)
      error(
        $i18n('{count} actions failed', { count: failed.length }) +
          ': ' +
          results.find((r) => r.status === 'rejected').reason.message
      )
    await loadFiles(get(currentPath))
    selected.set(new Set(failed))
    await loadFavorites().catch(() => {})
  }
  async function triggerReindex() {
    indexing = true
    showMenu = false
    try {
      await requireOK(await apiFetch('/api/files/reindex', { method: 'POST' }))
      success($i18n('Search indexing started'))
    } catch (err) {
      error(err.message)
    }
    reindexTimeout = setTimeout(() => (indexing = false), 2000)
  }
  async function moveFile(e, targetPath) {
    e.preventDefault()
    e.stopPropagation()
    dragOverPath = null
    if (e.dataTransfer?.files.length) {
      uploader?.upload(e.dataTransfer.files, targetPath)
      uploadDragging = false
      return
    }
    const src = e.dataTransfer?.getData('fileship/path')
    if (!src || src === targetPath || targetPath.startsWith(src + '/')) return
    let paths = [src]
    try {
      paths =
        JSON.parse(e.dataTransfer.getData('fileship/paths') || 'null') || paths
    } catch {}
    const mode = e.ctrlKey || e.metaKey ? 'copy' : 'move'
    try {
      const results = await transferFiles(paths, targetPath, mode)
      await transferred(results)
      const failed = results.filter((result) => !result.ok)
      if (failed.length) error(failed.map((result) => result.error).join('\n'))
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
      success($i18n('Archive extracted'))
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
          $i18n('Search failed')
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
  $: filters = [
    { id: 'all', label: $i18n('All files') },
    { id: 'folder', label: $i18n('Folders') },
    { id: 'text', label: $i18n('Documents') },
    { id: 'image', label: $i18n('Images') },
    { id: 'video', label: $i18n('Video') },
    { id: 'audio', label: $i18n('Audio') }
  ]
  $: favoritePaths = new Set($favorites.map((f) => f.path))
  $: isSearching = !!searchQuery.trim()
  $: displayFiles = (searchResults ?? $files.files ?? []).map((file) => ({
    ...file,
    starred: favoritePaths.has(file.path)
  }))
  $: filtered = displayFiles.filter(
    (file) =>
      (!$preferences.hideDotfiles || !file.name.startsWith('.')) &&
      (filter === 'all' ||
        (filter === 'starred' ? file.starred : fileKind(file) === filter))
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
    ? $i18n('Search results')
    : $currentPath
      ? $currentPath.split('/').pop()
      : $i18n('Your files')
  $: visibleError = searchError || $loadError
</script>

<svelte:window
  onkeydown={onKeydown}
  bind:innerWidth={viewportWidth}
  ondragover={(e) => {
    if (Array.from(e.dataTransfer?.types || []).includes('Files')) {
      e.preventDefault()
      uploadDragging = true
    }
  }}
  ondragleave={(e) => {
    if (!e.relatedTarget) uploadDragging = false
  }}
  ondrop={dropUpload}
/>
<a class="skip-link" href="#main-content">{$i18n('Skip to files')}</a>
<div class="layout">
  {#if mobileNav}<button
      class="nav-backdrop"
      aria-label={$i18n('Close navigation')}
      onclick={() => (mobileNav = false)}
    ></button>{/if}
  <aside
    class="sidebar"
    class:mobile-open={mobileNav}
    aria-label={$i18n('Workspace navigation')}
    inert={viewportWidth <= 800 && !mobileNav}
  >
    <button
      class="drawer-close"
      aria-label={$i18n('Close navigation')}
      onclick={() => (mobileNav = false)}><Icon name="x" size={18} /></button
    >
    <a class="brand" href="/" aria-label={$i18n('Fileship home')}
      ><Icon name="ship" size={29} /><strong>fileship<span>.</span></strong></a
    >
    <div class="workspace-label">
      <i></i>{$i18n('File manager')}
    </div>
    <nav class="sidebar-nav" aria-label={$i18n('Main navigation')}>
      <span class="nav-label">{$i18n('Library')}</span>
      <button
        class="nav-item"
        class:active={filter !== 'starred'}
        onclick={() => navigateTo('')}
        ><Icon name="folder" size={19} /><span>{$i18n('All files')}</span
        ></button
      >
      <button
        class="nav-item"
        onclick={() => {
          openCreate('folder')
          mobileNav = false
        }}
        ><Icon name="plus" size={19} /><span>{$i18n('New folder')}</span
        ></button
      >
      <button
        class="nav-item"
        onclick={() => {
          openCreate('file')
          mobileNav = false
        }}
        ><Icon name="file" size={19} /><span>{$i18n('New file')}</span></button
      >
      <button
        class="nav-item"
        onclick={() => {
          showShares = true
          mobileNav = false
        }}
        ><Icon name="share" size={19} /><span>{$i18n('Shared links')}</span
        ></button
      >
      <button
        class="nav-item"
        class:active={filter === 'starred'}
        onclick={() => setFilter('starred')}
        title={$i18n('Favorites in this folder')}
        ><Icon name="star" size={19} /><span>{$i18n('Favorites')}</span></button
      >
      <button
        class="nav-item"
        onclick={() => {
          showTrash = true
          mobileNav = false
        }}><Icon name="trash" size={19} /><span>{$i18n('Trash')}</span></button
      >
      <span class="nav-label favorites-label">{$i18n('Quick access')}</span>
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
          {$i18n('Star a file or folder to keep it close.')}
        </p>{/each}
    </nav>
    <div class="sidebar-bottom">
      {#if storage}<div class="storage-summary">
          <span>{$i18n('Storage used')}</span><strong
            >{formatBytes(storage.disk_usage, $locale)}</strong
          >{#if storage.quota_bytes > 0}<progress
              value={storage.disk_usage}
              max={storage.quota_bytes}
              aria-label={$i18n('Storage used')}
            ></progress><small
              >{formatBytes(storage.quota_bytes, $locale)}
              {$i18n('Quota')}</small
            >{/if}
        </div>{/if}
      <button
        class="nav-item"
        onclick={() => {
          showHelp = true
          mobileNav = false
        }}
        ><Icon name="info" size={18} /><span>{$i18n('Keyboard shortcuts')}</span
        ></button
      >
      {#if $user?.is_admin}<a class="nav-item" href="/admin"
          ><Icon name="users" size={18} /><span>{$i18n('Administration')}</span
          ></a
        >{/if}<button
        class="nav-item"
        onclick={() => {
          showSettings = true
          mobileNav = false
        }}
        ><Icon name="settings" size={18} /><span>{$i18n('Settings')}</span
        ></button
      ><button
        class="nav-item"
        onclick={() => {
          showThemePicker = true
          mobileNav = false
        }}
        ><Icon name="palette" size={18} /><span>{$i18n('Appearance')}</span
        ></button
      >
    </div>
    <div class="sidebar-user">
      <span class="avatar">{($user?.username?.[0] ?? '?').toUpperCase()}</span>
      <div>
        <strong>{$user?.username}</strong><span
          >{$user?.is_admin ? $i18n('Administrator') : $i18n('Member')}</span
        >
      </div>
      <button
        onclick={logout}
        aria-label={$i18n('Sign out')}
        title={$i18n('Sign out')}><Icon name="logout" size={17} /></button
      >
    </div>
  </aside>
  <main
    id="main-content"
    class="main"
    tabindex="-1"
    class:upload-dragging={uploadDragging}
    inert={mobileNav && viewportWidth <= 800}
  >
    {#if uploadDragging}<div class="upload-overlay">
        <Icon name="upload" size={35} />{$i18n('Drop files to upload')}
      </div>{/if}
    <header class="topbar">
      <button
        class="icon-btn mobile-menu"
        aria-label={$i18n('Open navigation')}
        aria-expanded={mobileNav}
        onclick={() => (mobileNav = !mobileNav)}
        ><Icon name="menu" size={21} /></button
      ><Breadcrumb path={$currentPath} onNavigate={navigateTo} />
      <div class="search-box">
        <Icon name="search" size={17} /><input
          bind:this={searchInput}
          bind:value={searchQuery}
          oninput={onSearch}
          placeholder={$i18n('Search your files…')}
          aria-label={$i18n('Search files')}
        />{#if isSearching}<button
            class="icon-btn"
            onclick={clearSearch}
            aria-label={$i18n('Clear search')}
            ><Icon name="x" size={15} /></button
          >{:else}<kbd>Ctrl K</kbd>{/if}
      </div>
      <span
        class="connection"
        class:offline={$connection !== 'connected'}
        title={$connection === 'connected'
          ? $i18n('Live updates connected')
          : $i18n('Live updates reconnecting')}
        ><i></i>{$connection === 'connected'
          ? $i18n('Live')
          : $i18n('Connecting')}</span
      >
    </header>
    <div class="workspace">
      <section class="page-heading">
        <div>
          <h1>{title}</h1>
        </div>
        <div class="heading-actions">
          <button class="btn" onclick={() => (infoPath = $currentPath)}
            ><Icon name="info" size={17} />{$i18n('Folder information')}</button
          >
          {#if clipboard}<button
              class="btn"
              onclick={() => startTransfer(clipboard.mode, clipboard.paths)}
              ><Icon name="clipboard" size={17} />{$i18n('Paste items')}</button
            >{/if}
          <button class="btn primary" onclick={() => uploader?.openPicker()}
            ><Icon name="upload" size={17} />{$i18n('Upload files')}</button
          >
        </div>
      </section>
      <Dropzone bind:this={uploader} expanded={showUpload} />
      <section class="file-section" aria-label={$i18n('File browser')}>
        <div class="file-toolbar">
          <div class="filter-tabs" aria-label={$i18n('Filter current page')}>
            {#each filters as item}<button
                class:active={filter === item.id}
                aria-pressed={filter === item.id}
                onclick={() => setFilter(item.id)}>{item.label}</button
              >{/each}{#if filter === 'starred'}<button
                class="active"
                onclick={() => setFilter('all')}
                >{$i18n('Favorites')} <Icon name="x" size={12} /></button
              >{/if}
          </div>
          <div class="toolbar-actions">
            <button
              class="icon-btn"
              aria-label={$i18n('Refresh folder')}
              title={$i18n('Refresh folder')}
              disabled={$loading}
              onclick={() => loadFiles($currentPath)}
              ><Icon name="refresh" size={17} /></button
            >
            <div class="view-toggle">
              <button
                class:active={viewMode === 'list'}
                aria-label={$i18n('List view')}
                aria-pressed={viewMode === 'list'}
                onclick={() => setView('list')}
                ><Icon name="list" size={17} /></button
              ><button
                class:active={viewMode === 'grid'}
                aria-label={$i18n('Grid view')}
                aria-pressed={viewMode === 'grid'}
                onclick={() => setView('grid')}
                ><Icon name="grid" size={17} /></button
              >
              <button
                class:active={viewMode === 'gallery'}
                aria-label={$i18n('Gallery view')}
                aria-pressed={viewMode === 'gallery'}
                onclick={() => setView('gallery')}
                ><Icon name="image" size={17} /></button
              >
            </div>
          </div>
        </div>
        <div class="list-meta">
          <span
            >{isSearching
              ? $i18n(
                  searchResults?.length === 1
                    ? '{count} result'
                    : '{count} results',
                  {
                    count: searchResults?.length ?? 0
                  }
                ) + (searchResults?.length >= 200 ? $i18n(' (first 200)') : '')
              : $i18n($files.total === 1 ? '{count} item' : '{count} items', {
                  count: $files.total
                })}<span class="meta-divider">/</span>{filter === 'all'
              ? $i18n('All types')
              : filter === 'starred'
                ? $i18n('Favorites on this page')
                : $i18n('{type} on this page', {
                    type: filters.find((f) => f.id === filter)?.label
                  })}</span
          >
          <div class="list-controls">
            <label
              >{$i18n('Sort by')}
              <select
                aria-label={$i18n('Sort files')}
                value={`${sortKey}:${sortAsc}`}
                onchange={(e) => {
                  const [key, asc] = e.target.value.split(':')
                  setSort(key, asc === 'true')
                }}
                ><option value="name:true">{$i18n('Name A–Z')}</option><option
                  value="name:false">{$i18n('Name Z–A')}</option
                ><option value="mod_time:false">{$i18n('Newest first')}</option
                ><option value="mod_time:true">{$i18n('Oldest first')}</option
                ><option value="size:false">{$i18n('Largest first')}</option
                ><option value="size:true">{$i18n('Smallest first')}</option
                ></select
              ></label
            >
            <div class="menu-wrap">
              <button
                class="icon-btn"
                aria-label={$i18n('More actions')}
                aria-expanded={showMenu}
                onclick={() => (showMenu = !showMenu)}
                ><Icon name="more" size={19} /></button
              >{#if showMenu}<button
                  class="menu-backdrop"
                  aria-label={$i18n('Close actions')}
                  onclick={() => (showMenu = false)}
                ></button>
                <div class="action-menu">
                  <button
                    onclick={() => {
                      showUpload = !showUpload
                      showMenu = false
                    }}
                    ><Icon name="upload" size={16} />{showUpload
                      ? $i18n('Hide upload area')
                      : $i18n('Show upload area')}</button
                  >
                  <button onclick={() => openCreate('file')}
                    ><Icon name="file" size={16} />{$i18n('New file')}</button
                  ><button
                    onclick={() => {
                      uploader?.openFolderPicker()
                      showMenu = false
                    }}
                    ><Icon name="folder" size={16} />{$i18n(
                      'Upload folder'
                    )}</button
                  ><button onclick={triggerReindex} disabled={indexing}
                    ><Icon name="refresh" size={16} />{indexing
                      ? $i18n('Indexing…')
                      : $i18n('Rebuild search index')}</button
                  >
                </div>{/if}
            </div>
          </div>
        </div>
        {#if selCount}<div class="selection-bar" role="status">
            <span>{$i18n('{count} selected', { count: selCount })}</span>
            <button onclick={() => startTransfer('copy')}
              ><Icon name="copy" size={16} />{$i18n('Copy')}</button
            >
            <button onclick={() => startTransfer('move')}
              ><Icon name="move" size={16} />{$i18n('Move')}</button
            >
            {#if selCount === 1}<button
                onclick={() => (infoPath = [...$selected][0])}
                ><Icon name="info" size={16} />{$i18n('Information')}</button
              >{/if}
            <button onclick={() => (archivePaths = [...$selected])}
              ><Icon name="download" size={16} />{$i18n('Download')}</button
            ><button
              class="danger"
              onclick={() => requestDelete([...$selected])}
              ><Icon name="trash" size={16} />{$i18n('Move to trash')}</button
            ><button
              class="clear-selection"
              aria-label={$i18n('Clear selection')}
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
                ? $i18n('Name your folder')
                : $i18n('Name your file')}
              aria-label={createKind === 'folder'
                ? $i18n('Folder name')
                : $i18n('File name')}
              required
            /><button class="btn primary" disabled={creating}
              >{creating ? $i18n('Creating…') : $i18n('Create')}</button
            ><button
              type="button"
              class="icon-btn"
              aria-label={$i18n('Cancel create')}
              onclick={() => (createKind = '')}
              ><Icon name="x" size={18} /></button
            >
          </form>{/if}
        <div
          class="file-wrap"
          class:grid-view={viewMode !== 'list'}
          bind:this={tableWrap}
          aria-busy={$loading || searchLoading}
        >
          {#if $loading || searchLoading}<Skeleton rows={8} />
          {:else if visibleError}<div class="empty-state" role="alert">
              <span class="empty-icon"><Icon name="warning" size={30} /></span>
              <h2>{$i18n('Something didn’t load')}</h2>
              <p>{visibleError}</p>
              <button
                class="btn"
                onclick={() =>
                  isSearching ? onSearch() : loadFiles($currentPath)}
                >{$i18n('Try again')}</button
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
                  ? $i18n('No files found')
                  : filter !== 'all'
                    ? $i18n('Nothing here yet')
                    : $i18n('This folder is empty')}
              </h2>
              <p>
                {isSearching
                  ? $i18n('No matches for “{query}”. Try another name.', {
                      query: searchQuery
                    })
                  : filter !== 'all'
                    ? $i18n('Try another filter or a different page.')
                    : $i18n(
                        'Upload your first file or create a folder to get started.'
                      )}
              </p>
              {#if isSearching}<button class="btn" onclick={clearSearch}
                  >{$i18n('Clear search')}</button
                >{:else if filter !== 'all'}<button
                  class="btn"
                  onclick={() => setFilter('all')}
                  >{$i18n('Show all files')}</button
                >{:else}<button
                  class="btn primary"
                  onclick={() => uploader?.openPicker()}
                  ><Icon name="upload" size={17} />{$i18n(
                    'Upload files'
                  )}</button
                >{/if}
            </div>
          {:else}<table>
              <thead
                ><tr
                  ><th class="col-check"
                    ><input
                      type="checkbox"
                      aria-label={$i18n('Select all visible files')}
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
                      >{$i18n('Name')}
                      {#if sortKey === 'name'}<Icon
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
                    ><button onclick={() => setSort('size')}
                      >{$i18n('Size')}</button
                    ></th
                  ><th
                    class="col-date"
                    aria-sort={sortKey === 'mod_time'
                      ? sortAsc
                        ? 'ascending'
                        : 'descending'
                      : 'none'}
                    ><button onclick={() => setSort('mod_time')}
                      >{$i18n('Last modified')}</button
                    ></th
                  ><th class="col-actions"
                    ><span class="sr-only">{$i18n('Actions')}</span></th
                  ></tr
                ></thead
              ><tbody
                >{#each sorted as file (file.path)}<FileRow
                    showPath={isSearching}
                    {file}
                    gallery={viewMode === 'gallery'}
                    onSelect={selectFile}
                    {dragOverPath}
                    onPreview={(f) => (previewFile = f)}
                    onShare={(f) => (shareFile = f)}
                    onEdit={(f) => (editorFile = f)}
                    onNavigate={navigateTo}
                    onUnzip={unzip}
                    onDelete={(f) => requestDelete([f.path])}
                    onCopy={(f) => startTransfer('copy', [f.path])}
                    onMove={(f) => startTransfer('move', [f.path])}
                    onInfo={(f) => (infoPath = f.path)}
                    onArchive={(f) => (archivePaths = [f.path])}
                    onDragOver={(e, path) => {
                      e.preventDefault()
                      e.dataTransfer.dropEffect =
                        Array.from(e.dataTransfer.types).includes('Files') ||
                        e.ctrlKey ||
                        e.metaKey
                          ? 'copy'
                          : 'move'
                      dragOverPath = path
                    }}
                    onDrop={moveFile}
                  />{/each}</tbody
              >
            </table>{/if}
        </div>
        <footer class="file-footer">
          <span
            >{isSearching
              ? $i18n('{count} shown', { count: sorted.length })
              : $i18n('{shown} of {total} items', {
                  shown: sorted.length,
                  total: $files.total
                })}</span
          >{#if !isSearching && totalPages > 1}<div class="pagination">
              <button
                class="icon-btn"
                aria-label={$i18n('Previous page')}
                disabled={$files.page <= 1 || $loading}
                onclick={() =>
                  loadFiles($currentPath, { page: $files.page - 1 })}
                ><Icon name="chevron_l" size={16} /></button
              ><span>{$i18n('Page')} {$files.page} / {totalPages}</span><button
                class="icon-btn"
                aria-label={$i18n('Next page')}
                disabled={$files.page >= totalPages || $loading}
                onclick={() =>
                  loadFiles($currentPath, { page: $files.page + 1 })}
                ><Icon name="chevron_r" size={16} /></button
              >
            </div>{:else}<span class="keyboard-hint"
              ><kbd>Ctrl K</kbd>
              {$i18n('to search')} <span>·</span>
              {$i18n('Drag files to a folder')}</span
            >{/if}
        </footer>
      </section>
    </div>
  </main>
</div>
<Preview
  file={previewFile}
  siblings={sorted.filter((f) => !f.is_dir)}
  onNavigate={(f) => (previewFile = f)}
  onClose={() => (previewFile = null)}
/><ShareDialog file={shareFile} onClose={() => (shareFile = null)} /><Editor
  file={editorFile}
  onClose={() => (editorFile = null)}
/>
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
{#if transfer}<TransferDialog
    {...transfer}
    onComplete={transferred}
    onClose={() => (transfer = null)}
  />{/if}
{#if infoPath !== null}<Properties
    path={infoPath}
    onClose={() => (infoPath = null)}
  />{/if}
{#if showShares}<Shares onClose={() => (showShares = false)} />{/if}
{#if showHelp}<Help onClose={() => (showHelp = false)} />{/if}
{#if archivePaths}<DownloadDialog
    paths={archivePaths}
    onClose={() => (archivePaths = null)}
  />{/if}
{#if deletePaths.length}<ConfirmDialog
    title={$i18n('Move {count} items to trash?', { count: deletePaths.length })}
    description={$i18n('You can restore these items from Trash for 30 days.')}
    confirmLabel={$i18n('Move to trash')}
    busy={deleting}
    onConfirm={confirmDelete}
    onClose={() => {
      if (!deleting) deletePaths = []
    }}
  />{/if}<Toast />

<style>
  .skip-link {
    position: fixed;
    left: 16px;
    top: -80px;
    z-index: 100;
    padding: 12px 18px;
    background: var(--surface);
    color: var(--accent);
    border-radius: 8px;
  }
  .skip-link:focus {
    top: 12px;
  }
  .upload-overlay {
    position: fixed;
    inset: 12px 12px 12px 264px;
    z-index: 30;
    background: var(--surface);
    opacity: 0.95;
    border: 2px dashed var(--accent);
    border-radius: 12px;
    pointer-events: none;
    display: flex;
    flex-direction: column;
    gap: 16px;
    align-items: center;
    justify-content: center;
    font-size: 20px;
    color: var(--accent);
  }
  @media (max-width: 800px) {
    .upload-overlay {
      left: 12px;
    }
  }
  .heading-actions {
    flex-wrap: wrap;
  }
  .selection-bar {
    flex-wrap: wrap;
  }
  .storage-summary {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 15px 14px;
    color: var(--sidebar-text);
    font-size: 12px;
  }
  .storage-summary strong {
    margin-left: auto;
    font-weight: 500;
  }
  .storage-summary progress {
    width: 100%;
    height: 5px;
    accent-color: var(--sidebar-active);
  }
  .storage-summary small {
    color: var(--sidebar-muted);
  }
  .drawer-close {
    display: none;
    position: absolute;
    right: 12px;
    top: 16px;
    background: none;
    border: 0;
    color: var(--sidebar-text);
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
    height: 100dvh;
    overflow: hidden;
  }
  .sidebar {
    width: 240px;
    flex-shrink: 0;
    height: 100dvh;
    position: sticky;
    top: 0;
    display: flex;
    flex-direction: column;
    padding: 22px 18px 0;
    background: var(--sidebar-bg);
    color: var(--sidebar-text);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 11px;
    color: var(--sidebar-active);
    padding: 0 12px;
    text-decoration: none;
  }
  .brand strong {
    font-size: 29px;
    letter-spacing: -1.4px;
    color: var(--sidebar-text);
  }
  .brand strong span {
    color: var(--sidebar-active);
  }
  .workspace-label {
    margin: 16px 12px 22px;
    color: var(--sidebar-text);
    font-size: 12px;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  .workspace-label i {
    width: 5px;
    height: 5px;
    background: var(--sidebar-active);
    border-radius: 50%;
  }
  .sidebar-nav {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }
  .nav-label {
    display: block;
    color: var(--sidebar-muted);
    font-size: 12px;
    letter-spacing: 0;
    padding: 0 14px 12px;
    font-weight: 600;
  }
  .favorites-label {
    padding-top: 20px;
  }
  .nav-item {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 9px 14px;
    margin: 2px 0;
    border: 0;
    background: transparent;
    border-radius: 8px;
    color: var(--sidebar-text);
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
    background: var(--sidebar-hover);
    color: white;
  }
  .nav-item.active {
    background: var(--sidebar-active);
    color: var(--sidebar-on-active);
    font-weight: 600;
  }
  .favorite-link {
    font-size: 12px;
    padding-top: 10px;
    padding-bottom: 10px;
  }
  .favorites-hint {
    color: var(--sidebar-muted);
    font-size: 12px;
    line-height: 1.8;
    margin: 0 14px;
    max-width: 170px;
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
    border-top: 1px solid var(--sidebar-border);
    margin: 0 -18px;
    padding: 18px 24px;
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    background: var(--sidebar-active);
    color: var(--sidebar-on-active);
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
    font-size: 12px;
    color: var(--sidebar-muted);
  }
  .sidebar-user button {
    margin-left: auto;
    background: none;
    border: 0;
    color: var(--sidebar-text);
    cursor: pointer;
    padding: 8px;
    border-radius: 5px;
  }
  .sidebar-user button:hover {
    background: var(--sidebar-hover);
  }
  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .topbar {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 22px;
    padding: 14px 28px;
    border-bottom: 1px solid var(--border);
    min-height: 65px;
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
    font-size: 12px;
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
    width: 100%;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: auto;
    max-width: 1600px;
    margin: 0 auto;
    padding: 22px 28px 20px;
  }
  .page-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    margin-bottom: 16px;
  }
  h1 {
    font-size: clamp(22px, 2vw, 28px);
    line-height: 1.3;
    letter-spacing: -0.6px;
    font-weight: 600;
    margin: 0;
    overflow-wrap: anywhere;
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
    flex: 1;
    min-height: 200px;
    display: flex;
    flex-direction: column;
    margin-top: 8px;
  }
  .file-toolbar {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    border-bottom: 1px solid var(--border);
  }
  .filter-tabs {
    min-width: 0;
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
    padding: 12px 0;
    gap: 10px;
    font-size: 12px;
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
    font-size: 12px;
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
    flex-wrap: wrap;
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
    font-size: 12px;
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
    flex: 1;
    min-height: 150px;
    overscroll-behavior: contain;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    overflow: auto;
  }
  table {
    table-layout: fixed;
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
    font-size: 12px;
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
    font-size: 12px;
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
      width: 240px;
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
      font-size: 12px;
    }
    .list-controls {
      gap: 5px;
    }
    .keyboard-hint {
      display: none;
    }
    .file-wrap {
      min-height: 180px;
    }
    .col-date {
      display: none;
    }
    :global(.file-wrap td.col-date) {
      display: none;
    }
  }
  @media (max-width: 600px) {
    .file-toolbar {
      flex-wrap: wrap;
    }
    .filter-tabs {
      width: 100%;
      gap: 20px;
    }
    .filter-tabs button {
      min-height: 44px;
      padding-bottom: 8px;
    }
    .toolbar-actions {
      margin-left: auto;
      padding-bottom: 8px;
    }
    .heading-actions {
      flex-wrap: wrap;
    }
    .icon-btn {
      min-height: 40px;
      min-width: 40px;
    }
    .view-toggle button {
      min-height: 36px;
      min-width: 36px;
    }
    .workspace {
      padding-top: 18px;
    }
    .list-meta {
      flex-wrap: wrap;
    }
  }
  @media (max-width: 480px) {
    th.col-actions {
      width: 84px;
    }
    th.col-check {
      width: 34px;
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
      font-size: 12px;
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
