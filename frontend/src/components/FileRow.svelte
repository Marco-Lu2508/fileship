<script module>
  let nextMenuId = 0
</script>

<script>
  import { tick } from 'svelte'
  import { get } from 'svelte/store'
  import {
    renameFile,
    loadFiles,
    currentPath,
    selected,
    downloadFile
  } from '../stores/files.js'
  import { success, error } from '../stores/toast.js'
  import { addFavorite, removeFavorite } from '../stores/favorites.js'
  import { fileKind, formatBytes, validName } from '../lib/files.js'
  import Icon from './Icon.svelte'
  export let file
  export let showPath = false
  export let onPreview = () => {},
    onShare = () => {},
    onEdit = () => {},
    onUnzip = () => {},
    onDelete = () => {}
  export let onNavigate = (path) => loadFiles(path),
    onDragOver = () => {},
    onDrop = () => {},
    dragOverPath = null
  let renaming = false,
    saving = false,
    newName = file.name,
    renameInput,
    menu,
    menuX = 0,
    menuY = 0
  const menuId = `file-menu-${++nextMenuId}`

  async function startRename() {
    menu?.hidePopover()
    newName = file.name
    renaming = true
    await tick()
    renameInput?.focus()
    renameInput?.select()
  }
  async function saveRename() {
    if (saving) return
    const name = newName.trim()
    if (!validName(name)) {
      error('Use a name without slashes or control characters')
      return
    }
    if (name === file.name) {
      renaming = false
      return
    }
    saving = true
    try {
      const parent = file.path.includes('/')
        ? file.path.slice(0, file.path.lastIndexOf('/'))
        : ''
      await renameFile(file.path, parent ? `${parent}/${name}` : name)
      renaming = false
      success('File renamed')
      await loadFiles(get(currentPath))
    } catch (err) {
      error(err.message)
    } finally {
      saving = false
    }
  }
  async function toggleFavorite() {
    try {
      if (file.starred) await removeFavorite(file.path)
      else await addFavorite(file.path, file.name, file.is_dir)
    } catch (err) {
      error(err.message)
    }
  }
  async function download() {
    menu?.hidePopover()
    try {
      await downloadFile(file.path, file.is_dir)
    } catch (err) {
      error(err.message)
    }
  }
  function action(callback) {
    menu?.hidePopover()
    callback(file)
  }
  function positionMenu(event) {
    const box = event.currentTarget.getBoundingClientRect()
    menuX = Math.max(8, Math.min(box.right - 190, innerWidth - 198))
    menuY = Math.max(8, Math.min(box.bottom + 5, innerHeight - 340))
  }
  function toggleSelect() {
    selected.update((set) => {
      const next = new Set(set)
      next.has(file.path) ? next.delete(file.path) : next.add(file.path)
      return next
    })
  }
  function dateLabel(value) {
    if (!value) return '—'
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return '—'
    return date.toLocaleDateString(undefined, {
      day: 'numeric',
      month: 'short',
      year: 'numeric'
    })
  }
  $: kind = fileKind(file)
  $: isSelected = $selected.has(file.path)
  $: isText =
    file.mime_type?.startsWith('text/') ||
    /\.(js|ts|json|md|ya?ml|toml|sh|py|go|rs|css|html|xml|csv|ini|conf|txt|log)$/i.test(
      file.name
    )
  $: isZip = /\.zip$/i.test(file.name)
  $: typeLabel = file.is_dir
    ? 'Folder'
    : file.name.includes('.')
      ? `${file.name.split('.').pop().toUpperCase()} file`
      : 'File'
</script>

<tr
  class="row"
  class:selected={isSelected}
  class:drag-over={file.is_dir && dragOverPath === file.path}
  draggable={!renaming}
  ondragstart={(e) => {
    e.dataTransfer.setData('fileship/path', file.path)
    e.dataTransfer.effectAllowed = 'move'
  }}
  ondragover={(e) => file.is_dir && onDragOver(e, file.path)}
  ondragleave={() => (dragOverPath = null)}
  ondrop={(e) => file.is_dir && onDrop(e, file.path)}
>
  <td class="col-check"
    ><input
      type="checkbox"
      onchange={toggleSelect}
      checked={isSelected}
      aria-label={`Select ${file.name}`}
    /></td
  >
  <td class="col-name"
    ><span class="file-icon-wrap kind-{kind}"
      ><Icon name={kind} size={19} /></span
    >{#if renaming}<form
        class="rename-form"
        onsubmit={(e) => {
          e.preventDefault()
          saveRename()
        }}
      >
        <input
          class="rename-input"
          bind:this={renameInput}
          bind:value={newName}
          aria-label="Rename file"
          onkeydown={(e) => {
            if (e.key === 'Escape') {
              e.stopPropagation()
              renaming = false
            }
          }}
        /><button class="act" disabled={saving} aria-label="Save name"
          ><Icon name="check" size={16} /></button
        ><button
          type="button"
          class="act"
          aria-label="Cancel rename"
          onclick={() => (renaming = false)}><Icon name="x" size={16} /></button
        >
      </form>{:else}<button
        class="name-btn"
        title={file.path}
        onclick={() => (file.is_dir ? onNavigate(file.path) : onPreview(file))}
        ><span class="name-text">{file.name}</span><span class="type-label"
          >{showPath ? file.path : typeLabel}</span
        ></button
      >{/if}</td
  >
  <td class="col-size"
    >{file.is_dir
      ? file.dir_size > 0
        ? formatBytes(file.dir_size)
        : '—'
      : formatBytes(file.size)}</td
  ><td class="col-date"
    ><span title={file.mod_time ? new Date(file.mod_time).toLocaleString() : ''}
      >{dateLabel(file.mod_time)}</span
    ></td
  >
  <td class="col-actions"
    ><div class="action-group">
      <button
        class="act star"
        class:starred={file.starred}
        onclick={toggleFavorite}
        aria-label={file.starred ? `Unstar ${file.name}` : `Star ${file.name}`}
        title={file.starred ? 'Remove from favorites' : 'Add to favorites'}
        ><Icon name="star" size={16} /></button
      ><button
        class="act download"
        onclick={download}
        aria-label={`Download ${file.name}`}
        title={file.is_dir ? 'Download as ZIP' : 'Download'}
        ><Icon name="download" size={16} /></button
      ><button
        class="act"
        popovertarget={menuId}
        onclick={positionMenu}
        aria-label={`Actions for ${file.name}`}
        title="More actions"><Icon name="more" size={18} /></button
      >
    </div>
    <div
      id={menuId}
      bind:this={menu}
      popover
      class="row-menu"
      style={`left:${menuX}px;top:${menuY}px`}
      aria-label={`Actions for ${file.name}`}
    >
      {#if !file.is_dir}<button onclick={() => action(onPreview)}
          ><Icon name="eye" size={16} />Preview</button
        >{/if}<button onclick={download}
        ><Icon name="download" size={16} />{file.is_dir
          ? 'Download ZIP'
          : 'Download'}</button
      >{#if isText}<button onclick={() => action(onEdit)}
          ><Icon name="edit" size={16} />Edit file</button
        >{/if}{#if isZip}<button onclick={() => action(onUnzip)}
          ><Icon name="extract" size={16} />Extract ZIP</button
        >{/if}<button onclick={() => action(onShare)}
        ><Icon name="share" size={16} />Share link</button
      ><button onclick={startRename}
        ><Icon name="rename" size={16} />Rename</button
      ><button class="danger" onclick={() => action(onDelete)}
        ><Icon name="trash" size={16} />Move to trash</button
      >
    </div></td
  >
</tr>

<style>
  .row {
    border-bottom: 1px solid var(--border2);
    transition: background 0.15s;
  }
  .row:last-child {
    border-bottom: 0;
  }
  .row:hover {
    background: var(--row-hover);
  }
  .row.selected {
    background: var(--row-active);
  }
  .row.drag-over {
    outline: 2px dashed var(--accent);
    outline-offset: -2px;
    background: var(--accent-soft);
  }
  td {
    padding: 14px 17px;
    vertical-align: middle;
  }
  .col-check {
    width: 48px;
  }
  .col-name {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .file-icon-wrap {
    width: 36px;
    height: 40px;
    display: grid;
    place-items: center;
    border-radius: 8px;
    flex-shrink: 0;
    color: var(--text2);
    background: var(--surface2);
  }
  .kind-folder {
    color: #a88737;
    background: #f8f2df;
  }
  .kind-image {
    color: #7d65ac;
    background: #f0eaf9;
  }
  .kind-video {
    color: #b77654;
    background: #fbede4;
  }
  .kind-audio {
    color: #639484;
    background: #e7f2ec;
  }
  .kind-text {
    color: #6085af;
    background: #ebf1f9;
  }
  .kind-archive {
    color: #a98539;
    background: #f8f0df;
  }
  .name-btn {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 3px;
    flex: 1;
    min-width: 0;
    text-align: left;
    border: 0;
    background: none;
    color: var(--text);
    cursor: pointer;
  }
  .name-text {
    font-size: 12px;
    font-weight: 500;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .name-btn:hover .name-text {
    color: var(--accent);
  }
  .type-label {
    font-size: 9px;
    color: var(--text3);
  }
  .col-size,
  .col-date {
    color: var(--text2);
    font-size: 10px;
    white-space: nowrap;
  }
  .action-group {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 5px;
  }
  .act {
    display: grid;
    place-items: center;
    border: 0;
    background: none;
    color: var(--text3);
    padding: 7px;
    border-radius: 5px;
    cursor: pointer;
  }
  .act:hover {
    background: var(--surface3);
    color: var(--text);
  }
  .star.starred {
    color: var(--warning);
  }
  .star.starred :global(svg) {
    fill: var(--warning);
  }
  .download {
    opacity: 0;
  }
  .row:hover .download,
  .row:focus-within .download,
  .row.selected .download {
    opacity: 1;
  }
  .row-menu {
    position: fixed;
    margin: 0;
    width: 190px;
    padding: 6px;
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 9px;
    box-shadow: var(--shadow-lg);
  }
  .row-menu button {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 10px;
    font-size: 12px;
    background: none;
    color: var(--text);
    border: 0;
    border-radius: 5px;
    cursor: pointer;
    text-align: left;
  }
  .row-menu button:hover {
    background: var(--row-hover);
  }
  .row-menu button.danger {
    color: var(--danger);
    border-top: 1px solid var(--border);
    border-radius: 0;
    margin-top: 4px;
  }
  .rename-form {
    display: flex;
    flex: 1;
    gap: 4px;
    min-width: 0;
  }
  .rename-input {
    width: 100%;
    min-width: 0;
    background: var(--input-bg);
    color: var(--text);
    border: 1px solid var(--accent);
    border-radius: 6px;
    padding: 7px;
    font-size: 12px;
  }
  @media (hover: none) {
    .download {
      opacity: 1;
    }
    .act {
      min-height: 38px;
    }
  }
  @media (max-width: 600px) {
    td {
      padding: 12px;
    }
    .col-name {
      gap: 8px;
    }
    .download {
      display: none;
    }
    .file-icon-wrap {
      width: 31px;
      height: 35px;
    }
    .col-check {
      width: 33px;
    }
    .action-group {
      gap: 0;
    }
    .act {
      padding: 6px;
    }
    .name-text {
      max-width: 40vw;
    }
    .type-label {
      font-size: 8px;
    }
  }
</style>
