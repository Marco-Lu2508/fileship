<script>
  import { deleteFile, renameFile, loadFiles, currentPath, selected, downloadUrl, zipUrl } from '../stores/files.js'
  import { success, error } from '../stores/toast.js'
  import { get } from 'svelte/store'

  export let file
  export let onPreview = () => {}
  export let onShare = () => {}
  export let onEdit = () => {}
  export let onUnzip = () => {}
  export let onDragOver = () => {}
  export let onDrop = () => {}
  export let dragOverPath = null

  let renaming = false
  let newName = file.name
  let dragging = false

  const icons = {
    dir: '📁', image: '🖼️', video: '🎬', audio: '🎵',
    pdf: '📄', zip: '🗜️', text: '📝', default: '📄'
  }

  function getIcon(f) {
    if (f.is_dir) return icons.dir
    const m = f.mime_type || ''
    if (m.startsWith('image/')) return icons.image
    if (m.startsWith('video/')) return icons.video
    if (m.startsWith('audio/')) return icons.audio
    if (m.includes('pdf')) return icons.pdf
    if (m.includes('zip') || m.includes('tar') || m.includes('gzip')) return icons.zip
    if (m.startsWith('text/')) return icons.text
    return icons.default
  }

  $: isImage = file.mime_type?.startsWith('image/')
  $: thumbUrl = isImage ? `/api/files/thumb?path=${encodeURIComponent(file.path)}&token=${localStorage.getItem('access_token')}` : null

  function formatSize(bytes) {
    if (!bytes) return '—'
    const units = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return (bytes / Math.pow(1024, i)).toFixed(1) + ' ' + units[i]
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
    } catch {
      error('Delete failed')
    }
  }

  async function handleRename() {
    if (!renaming) { renaming = true; return }
    if (newName && newName !== file.name) {
      try {
        const dir = file.path.includes('/') ? file.path.substring(0, file.path.lastIndexOf('/')) : ''
        await renameFile(file.path, dir ? dir + '/' + newName : newName)
        await loadFiles(get(currentPath))
        success('Renamed')
      } catch {
        error('Rename failed')
      }
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

  function handleNameClick() {
    if (file.is_dir) {
      import('../stores/files.js').then(m => m.loadFiles(file.path))
    } else {
      onPreview(file)
    }
  }
</script>

<tr
  class="file-row"
  class:drag-over={file.is_dir && dragOverPath === file.path}
  draggable="true"
  ondragstart={(e) => { dragging = true; e.dataTransfer.setData('fileship/path', file.path) }}
  ondragend={() => dragging = false}
  ondragover={(e) => file.is_dir && onDragOver(e, file.path)}
  ondragleave={() => onDragOver(null, null)}
  ondrop={(e) => file.is_dir && onDrop(e, file.path)}
>
  <td class="check">
    <input type="checkbox" onchange={toggleSelect} checked={$selected.has(file.path)} />
  </td>
  <td class="name-cell">
    {#if renaming}
      <input
        class="rename-input"
        bind:value={newName}
        onkeydown={(e) => { if (e.key === 'Enter') handleRename(); if (e.key === 'Escape') { renaming = false; newName = file.name } }}
        onblur={handleRename}
        autofocus
      />
    {:else}
      <span class="icon">
        {#if thumbUrl}
          <img class="thumb" src={thumbUrl} alt={file.name} loading="lazy" />
        {:else}
          {getIcon(file)}
        {/if}
      </span>
      <button class="name-btn" onclick={handleNameClick}>{file.name}</button>
    {/if}
  </td>
  <td class="size">{file.is_dir ? '—' : formatSize(file.size)}</td>
  <td class="date">{formatDate(file.mod_time)}</td>
  <td class="actions">
    {#if file.is_dir}
      <a class="btn" href={zipUrl(file.path)} title="Download as ZIP">🗜️</a>
    {:else}
      <button class="btn" onclick={() => onPreview(file)} title="Preview">👁️</button>
      <a class="btn" href={downloadUrl(file.path)} title="Download">⬇️</a>
      {#if file.mime_type?.startsWith('text/') || ['js','ts','json','md','yaml','yml','toml','sh','py','go','rs','css','html','xml','csv'].some(e => file.name.endsWith('.'+e))}
        <button class="btn" onclick={() => onEdit(file)} title="Edit">✏️</button>
      {/if}
      {#if file.name.endsWith('.zip')}
        <button class="btn" onclick={() => onUnzip(file)} title="Extract ZIP">📦</button>
      {/if}
    {/if}
    <button class="btn" onclick={() => onShare(file)} title="Share">🔗</button>
    <button class="btn" onclick={handleRename} title="Rename">🖊️</button>
    <button class="btn danger" onclick={handleDelete} title="Delete">🗑️</button>
  </td>
</tr>

<style>
  .file-row:hover { background: var(--surface, #1e2035); }
  .file-row.drag-over { background: #1e2035; outline: 2px dashed var(--accent, #5865f2); }
  td { padding: 0.6rem 0.75rem; border-bottom: 1px solid var(--bg, #1a1d27); vertical-align: middle; }
  .check { width: 2rem; }
  .name-cell { display: flex; align-items: center; gap: 0.5rem; }
  .icon { font-size: 1.1rem; flex-shrink: 0; width: 1.5rem; height: 1.5rem; display: flex; align-items: center; justify-content: center; }
  .thumb { width: 1.5rem; height: 1.5rem; object-fit: cover; border-radius: 3px; }
  .name-btn { background: none; border: none; color: var(--text, #e2e8f0); cursor: pointer; font-size: 0.95rem; text-align: left; padding: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 40vw; }
  .name-btn:hover { color: var(--accent, #5865f2); text-decoration: underline; }
  .size, .date { color: var(--muted, #718096); font-size: 0.85rem; white-space: nowrap; }
  .actions { display: flex; gap: 0.25rem; flex-wrap: nowrap; }
  .btn { background: none; border: none; cursor: pointer; padding: 0.25rem 0.4rem; border-radius: 4px; font-size: 1rem; text-decoration: none; white-space: nowrap; }
  .btn:hover { background: #2a2d3a; }
  .btn.danger:hover { background: #3d1515; }
  .rename-input { background: #0f1117; border: 1px solid #5865f2; color: #fff; border-radius: 4px; padding: 0.2rem 0.4rem; font-size: 0.95rem; width: 100%; }
</style>
