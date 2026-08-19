<script>
  import { deleteFile, renameFile, loadFiles, currentPath, selected, downloadFile } from '../stores/files.js'
  import { success, error } from '../stores/toast.js'
  import { addFavorite, removeFavorite } from '../stores/favorites.js'
  import { get } from 'svelte/store'
  import Icon from './Icon.svelte'

  export let file
  export let rowIndex     = 0
  export let onPreview    = () => {}
  export let onShare      = () => {}
  export let onEdit       = () => {}
  export let onUnzip      = () => {}
  export let onNavigate   = (path) => loadFiles(path)
  export let onDragOver   = () => {}
  export let onDrop       = () => {}
  export let dragOverPath = null

  let renaming = false
  let newName  = file.name

  // ── Icon mapping ──────────────────────────────────
  function fileIcon(f) {
    if (f.is_dir) return 'folder'
    const m = f.mime_type || ''
    const n = f.name.toLowerCase()
    if (m.startsWith('image/'))  return 'image'
    if (m.startsWith('video/'))  return 'video'
    if (m.startsWith('audio/'))  return 'audio'
    if (m.includes('pdf'))       return 'text'
    if (m.includes('zip') || m.includes('tar') || m.includes('gzip') || n.endsWith('.zip')) return 'zip'
    if (m.startsWith('text/'))   return 'text'
    return 'file'
  }

  function fileIconColor(f) {
    if (f.is_dir) return 'dir'
    const m = f.mime_type || ''
    if (m.startsWith('image/'))  return 'img'
    if (m.startsWith('video/'))  return 'vid'
    if (m.startsWith('audio/'))  return 'aud'
    if (m.includes('pdf'))       return 'pdf'
    if (m.includes('zip') || m.includes('tar')) return 'arc'
    if (m.startsWith('text/') || m.startsWith('application/json')) return 'txt'
    return 'def'
  }

  // ── Size formatting ───────────────────────────────
  function formatSize(bytes, isDir, dirSize) {
    if (isDir) {
      if (!dirSize) return '—'
      return fmtBytes(dirSize) + ' total'
    }
    if (!bytes) return '—'
    return fmtBytes(bytes)
  }

  function fmtBytes(b) {
    if (b < 1024) return b + ' B'
    const u = ['KB','MB','GB','TB']
    const i = Math.min(Math.floor(Math.log(b) / Math.log(1024)), u.length - 1)
    return (b / Math.pow(1024, i)).toFixed(i > 1 ? 1 : 0) + '\u00a0' + u[i - 1]
  }

  function formatDate(d) {
    const dt = new Date(d)
    const now = new Date()
    const diff = now - dt
    const days = Math.floor(diff / 86400000)
    if (days === 0) return 'Today, ' + dt.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
    if (days === 1) return 'Yesterday'
    if (days < 7)   return dt.toLocaleDateString(undefined, { weekday: 'short', hour: '2-digit', minute: '2-digit' })
    if (days < 365) return dt.toLocaleDateString(undefined, { day: '2-digit', month: 'short' })
    return dt.toLocaleDateString(undefined, { day: '2-digit', month: 'short', year: 'numeric' })
  }

  // ── Actions ───────────────────────────────────────
  async function handleDelete() {
    if (!confirm(`Delete "${file.name}"?`)) return
    try {
      await deleteFile(file.path)
      await loadFiles(get(currentPath))
      success(`Deleted "${file.name}"`)
    } catch { error('Delete failed') }
  }

  async function handleRename() {
    if (!renaming) {
      renaming = true
      return
    }
    const trimmed = newName.trim()
    if (trimmed && trimmed !== file.name) {
      try {
        const dir = file.path.includes('/') ? file.path.substring(0, file.path.lastIndexOf('/')) : ''
        await renameFile(file.path, dir ? dir + '/' + trimmed : trimmed)
        await loadFiles(get(currentPath))
        success('Renamed')
      } catch { error('Rename failed') }
    }
    renaming = false
  }

  async function toggleFavorite() {
    if (file.starred) {
      await removeFavorite(file.path)
      success(`Removed from favorites`)
    } else {
      await addFavorite(file.path, file.name, file.is_dir)
      success(`Added to favorites`)
    }
  }

  function toggleSelect() {
    selected.update(s => {
      const n = new Set(s)
      n.has(file.path) ? n.delete(file.path) : n.add(file.path)
      return n
    })
  }

  function cancelRename() {
    renaming = false
    newName = file.name
  }
  $: isImage = file.mime_type?.startsWith('image/')
  $: isText  = file.mime_type?.startsWith('text/') ||
    ['js','ts','json','md','yaml','yml','toml','sh','py','go','rs','css','html','xml','csv','ini','conf','txt','log']
    .some(e => file.name.endsWith('.' + e))
  $: isZip   = file.name.toLowerCase().endsWith('.zip')
  $: iconColor = fileIconColor(file)
</script>

<tr
  class="row"
  class:selected={isSelected}
  class:drag-over={file.is_dir && dragOverPath === file.path}
  style="animation-delay: {Math.min(rowIndex * 18, 300)}ms"
  draggable="true"
  ondragstart={(e) => e.dataTransfer.setData('fileship/path', file.path)}
  ondragover={(e) => file.is_dir && onDragOver(e, file.path)}
  ondragleave={() => onDragOver(null, null)}
  ondrop={(e) => file.is_dir && onDrop(e, file.path)}
>
  <!-- Checkbox -->
  <td class="col-check">
    <input type="checkbox" onchange={toggleSelect} checked={isSelected} aria-label="Select {file.name}" />
  </td>

  <!-- Name -->
  <td class="col-name">
    {#if renaming}
      <input
        class="rename-input"
        bind:value={newName}
        onkeydown={(e) => {
          if (e.key === 'Enter') handleRename()
          if (e.key === 'Escape') cancelRename()
        }}
        onblur={handleRename}
        aria-label="Rename file"
      />
    {:else}
      <span class="file-icon-wrap color-{iconColor}">
        <Icon name={fileIcon(file)} size={14} />
      </span>
      {#if file.is_dir}
        <button class="name-btn" onclick={() => onNavigate(file.path)}>
          <span class="name-text">{file.name}</span>
        </button>
      {:else}
        <button class="name-btn" onclick={() => onPreview(file)}>
          <span class="name-text">{file.name}</span>
        </button>
      {/if}
    {/if}
  </td>

  <!-- Size (+ folder total) -->
  <td class="col-size">
    <span class="size-text" class:has-dir-size={file.is_dir && file.dir_size > 0}>
      {formatSize(file.size, file.is_dir, file.dir_size)}
    </span>
  </td>

  <!-- Modified -->
  <td class="col-date hide-mobile">
    <span class="date-text" title={new Date(file.mod_time).toLocaleString()}>
      {formatDate(file.mod_time)}
    </span>
  </td>

  <!-- Actions -->
  <td class="col-actions">
    <div class="action-group">
      {#if file.is_dir}
        <button class="act" onclick={() => downloadFile(file.path, true)} title="Download as ZIP">
          <Icon name="download" size={13} />
        </button>
      {:else}
        <button class="act" onclick={() => onPreview(file)} title="Preview">
          <Icon name="eye" size={13} />
        </button>
        <button class="act" onclick={() => downloadFile(file.path)} title="Download">
          <Icon name="download" size={13} />
        </button>
        {#if isText}
          <button class="act" onclick={() => onEdit(file)} title="Edit">
            <Icon name="edit" size={13} />
          </button>
        {/if}
        {#if isZip}
          <button class="act" onclick={() => onUnzip(file)} title="Extract ZIP">
            <Icon name="extract" size={13} />
          </button>
        {/if}
      {/if}
      <button class="act" onclick={() => onShare(file)} title="Share">
        <Icon name="share" size={13} />
      </button>
      <button class="act star" class:starred={file.starred} onclick={toggleFavorite}
        title={file.starred ? 'Remove from favorites' : 'Add to favorites'}>
        <Icon name={file.starred ? 'check' : 'save'} size={13} />
      </button>
      <button class="act" onclick={handleRename} title="Rename">
        <Icon name="rename" size={13} />
      </button>
      <div class="act-divider"></div>
      <button class="act danger" onclick={handleDelete} title="Delete">
        <Icon name="trash" size={13} />
      </button>
    </div>
  </td>
</tr>

<style>
  .row {
    border-bottom: 1px solid var(--border2);
    transition: background var(--transition);
    animation: row-in 0.2s ease both;
  }

  @keyframes row-in {
    from { opacity: 0; transform: translateX(-4px); }
    to   { opacity: 1; transform: none; }
  }

  .row:hover { background: var(--row-hover); }
  .row.selected { background: var(--row-active); }
  .row.drag-over {
    background: var(--accent-soft);
    outline: 2px dashed var(--accent);
    outline-offset: -2px;
  }

  td {
    padding: 0.62rem 1rem;
    vertical-align: middle;
  }

  /* Checkbox */
  .col-check { width: 2.5rem; }

  /* Name */
  .col-name {
    display: flex;
    align-items: center;
    gap: 0.55rem;
    min-width: 0;
  }

  .file-icon-wrap {
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    border-radius: 7px;
    flex-shrink: 0;
    transition: transform var(--transition);
  }

  .row:hover .file-icon-wrap { transform: scale(1.06); }

  /* Icon colours */
  .color-dir { background: rgba(79,158,255,0.12); color: var(--accent); }
  .color-img { background: rgba(236,107,177,0.12); color: #ec6bb1; }
  .color-vid { background: rgba(138,100,255,0.12); color: #8a64ff; }
  .color-aud { background: rgba(71,196,160,0.12);  color: #47c4a0; }
  .color-pdf { background: rgba(255,95,95,0.12);   color: #ff5f5f; }
  .color-arc { background: rgba(255,171,64,0.12);  color: #ffab40; }
  .color-txt { background: rgba(94,199,138,0.12);  color: var(--success); }
  .color-def { background: var(--surface2); color: var(--text3); }

  .name-btn {
    background: none;
    border: none;
    color: var(--text);
    cursor: pointer;
    text-align: left;
    padding: 0;
    min-width: 0;
    flex: 1;
    display: flex;
    align-items: center;
  }

  .name-text {
    font-size: 0.875rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 38vw;
    transition: color var(--transition);
  }

  .name-btn:hover .name-text { color: var(--accent); }

  .rename-input {
    background: var(--input-bg);
    border: 1px solid var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
    color: var(--text);
    border-radius: var(--radius);
    padding: 0.25rem 0.5rem;
    font-size: 0.875rem;
    width: 100%;
    min-width: 120px;
  }

  /* Size */
  .col-size { color: var(--text3); font-size: 0.8rem; white-space: nowrap; }
  .size-text.has-dir-size { color: var(--text2); font-size: 0.78rem; }

  /* Date */
  .col-date { white-space: nowrap; }
  .date-text { font-size: 0.78rem; color: var(--text3); }

  /* Actions */
  .col-actions { padding-right: 0.75rem; }
  .action-group {
    display: flex;
    align-items: center;
    gap: 2px;
    opacity: 0;
    transition: opacity var(--transition);
  }

  .row:hover .action-group,
  .row.selected .action-group {
    opacity: 1;
  }

  .act {
    background: none;
    border: none;
    color: var(--text2);
    cursor: pointer;
    padding: 0.32rem;
    border-radius: 5px;
    display: grid;
    place-items: center;
    transition: background var(--transition), color var(--transition);
  }

  .act:hover { background: var(--surface2); color: var(--text); }
  .act.danger:hover { background: var(--danger-bg); color: var(--danger); }

  .act.star { color: var(--text3); }
  .act.star.starred { color: var(--warning); }
  .act.star:hover { background: rgba(245,166,35,0.1); color: var(--warning); }

  .act-divider {
    width: 1px;
    height: 14px;
    background: var(--border);
    margin: 0 2px;
  }

  /* Grid overrides */
  :global(.grid-view) .action-group { opacity: 1; }

  @media (max-width: 680px) {
    .hide-mobile { display: none !important; }
    .name-text { max-width: 50vw; }
  }
</style>
