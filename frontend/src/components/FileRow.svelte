<script>
  import { deleteFile, renameFile, loadFiles, currentPath, selected, downloadUrl, zipUrl } from '../stores/files.js'
  import { success, error } from '../stores/toast.js'
  import { get } from 'svelte/store'
  import Icon from './Icon.svelte'

  export let file
  export let onPreview = () => {}
  export let onShare   = () => {}
  export let onEdit    = () => {}
  export let onUnzip   = () => {}
  export let onDragOver = () => {}
  export let onDrop    = () => {}
  export let dragOverPath = null

  let renaming = false
  let newName  = file.name

  function fileIcon(f) {
    if (f.is_dir) return 'folder'
    const m = f.mime_type || ''
    if (m.startsWith('image/'))  return 'image'
    if (m.startsWith('video/'))  return 'video'
    if (m.startsWith('audio/'))  return 'audio'
    if (m.includes('pdf'))       return 'text'
    if (m.includes('zip') || m.includes('tar') || m.includes('gzip')) return 'zip'
    if (m.startsWith('text/'))   return 'text'
    return 'file'
  }

  function formatSize(bytes) {
    if (!bytes || file.is_dir) return '—'
    const u = ['B','KB','MB','GB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return (bytes / Math.pow(1024, i)).toFixed(1) + '\u00a0' + u[i]
  }

  function formatDate(d) {
    return new Date(d).toLocaleDateString(undefined, { day: '2-digit', month: 'short', year: 'numeric' })
  }

  async function handleDelete() {
    if (!confirm(`Delete "${file.name}"?`)) return
    try {
      await deleteFile(file.path)
      await loadFiles(get(currentPath))
      success(`Deleted "${file.name}"`)
    } catch { error('Delete failed') }
  }

  async function handleRename() {
    if (!renaming) { renaming = true; return }
    if (newName && newName !== file.name) {
      try {
        const dir = file.path.includes('/') ? file.path.substring(0, file.path.lastIndexOf('/')) : ''
        await renameFile(file.path, dir ? dir + '/' + newName : newName)
        await loadFiles(get(currentPath))
        success('Renamed')
      } catch { error('Rename failed') }
    }
    renaming = false
  }

  function toggleSelect() {
    selected.update(s => {
      const n = new Set(s)
      n.has(file.path) ? n.delete(file.path) : n.add(file.path)
      return n
    })
  }

  $: isImage = file.mime_type?.startsWith('image/')
  $: isText  = file.mime_type?.startsWith('text/') ||
    ['js','ts','json','md','yaml','yml','toml','sh','py','go','rs','css','html','xml','csv','ini','conf']
    .some(e => file.name.endsWith('.' + e))
  $: isZip   = file.name.endsWith('.zip')
</script>

<tr
  class="row"
  class:drag-over={file.is_dir && dragOverPath === file.path}
  draggable="true"
  ondragstart={(e) => e.dataTransfer.setData('fileship/path', file.path)}
  ondragover={(e) => file.is_dir && onDragOver(e, file.path)}
  ondragleave={() => onDragOver(null, null)}
  ondrop={(e) => file.is_dir && onDrop(e, file.path)}
>
  <td class="col-check">
    <input type="checkbox" onchange={toggleSelect} checked={$selected.has(file.path)} />
  </td>

  <td class="col-name">
    {#if renaming}
      <input
        class="rename-input"
        bind:value={newName}
        onkeydown={(e) => { if (e.key === 'Enter') handleRename(); if (e.key === 'Escape') { renaming = false; newName = file.name } }}
        onblur={handleRename}
      />
    {:else}
      <span class="file-icon" class:is-dir={file.is_dir}>
        <Icon name={fileIcon(file)} size={15} />
      </span>
      {#if file.is_dir}
        <button class="name-btn" onclick={() => loadFiles(file.path)}>{file.name}</button>
      {:else}
        <button class="name-btn" onclick={() => onPreview(file)}>{file.name}</button>
      {/if}
    {/if}
  </td>

  <td class="col-size">{formatSize(file.size)}</td>
  <td class="col-date hide-mobile">{formatDate(file.mod_time)}</td>

  <td class="col-actions">
    {#if file.is_dir}
      <a class="action-btn" href={zipUrl(file.path)} title="Download as ZIP"><Icon name="download" size={14} /></a>
    {:else}
      <button class="action-btn" onclick={() => onPreview(file)} title="Preview"><Icon name="eye" size={14} /></button>
      <a class="action-btn" href={downloadUrl(file.path)} title="Download"><Icon name="download" size={14} /></a>
      {#if isText}
        <button class="action-btn" onclick={() => onEdit(file)} title="Edit"><Icon name="edit" size={14} /></button>
      {/if}
      {#if isZip}
        <button class="action-btn" onclick={() => onUnzip(file)} title="Extract"><Icon name="extract" size={14} /></button>
      {/if}
    {/if}
    <button class="action-btn" onclick={() => onShare(file)} title="Share"><Icon name="share" size={14} /></button>
    <button class="action-btn" onclick={handleRename} title="Rename"><Icon name="rename" size={14} /></button>
    <button class="action-btn danger" onclick={handleDelete} title="Delete"><Icon name="trash" size={14} /></button>
  </td>
</tr>

<style>
  .row { border-bottom: 1px solid var(--border); }
  .row:hover { background: var(--row-hover); }
  .row.drag-over { background: var(--row-hover); outline: 2px dashed var(--accent); outline-offset: -2px; }
  td { padding: 0.45rem 0.75rem; vertical-align: middle; }
  .col-check { width: 2rem; }
  .col-name { display: flex; align-items: center; gap: 0.5rem; min-width: 0; }
  .file-icon { color: var(--text2); display: flex; flex-shrink: 0; }
  .file-icon.is-dir { color: var(--accent); }
  .name-btn {
    background: none; border: none; color: var(--text);
    cursor: pointer; font-size: 0.875rem; text-align: left; padding: 0;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 40vw;
  }
  .name-btn:hover { color: var(--accent); text-decoration: underline; }
  .col-size, .col-date { color: var(--text2); font-size: 0.8rem; white-space: nowrap; }
  .col-actions { display: flex; gap: 0.1rem; align-items: center; }
  .action-btn {
    background: none; border: none; color: var(--text2);
    cursor: pointer; padding: 0.3rem; border-radius: 3px;
    display: flex; align-items: center; text-decoration: none;
  }
  .action-btn:hover { background: var(--border); color: var(--text); }
  .action-btn.danger:hover { background: var(--danger-bg); color: var(--danger); }
  .rename-input {
    background: var(--input-bg); border: 1px solid var(--accent);
    color: var(--text); border-radius: 3px; padding: 0.2rem 0.4rem;
    font-size: 0.875rem; width: 100%;
  }
  @media (max-width: 640px) { .hide-mobile { display: none; } }
</style>
