<script>
  import { translator as i18n } from '../stores/i18n.js'
  import { onMount, onDestroy } from 'svelte'
  import Dialog from './Dialog.svelte'
  import Icon from './Icon.svelte'
  import Breadcrumb from './Breadcrumb.svelte'
  import { apiFetch } from '../stores/auth.js'
  import { requireOK, validName } from '../lib/files.js'
  import { transferFiles, topLevelPaths } from '../lib/operations.js'
  export let paths = []
  export let mode = 'copy'
  export let initialPath = ''
  export let onClose = () => {}
  export let onComplete = () => {}
  let items = topLevelPaths(paths),
    folder = initialPath,
    folderInput = initialPath,
    name = items.length === 1 ? items[0].split('/').pop() : ''
  let entries = [],
    page = 1,
    totalPages = 1,
    loading = false,
    busy = false,
    issue = '',
    controller
  let newFolderName = ''
  async function createFolder() {
    if (!validName(newFolderName)) {
      issue = $i18n('Use a name without slashes or control characters')
      return
    }
    busy = true
    try {
      const path = folder
        ? `${folder}/${newFolderName.trim()}`
        : newFolderName.trim()
      await requireOK(
        await apiFetch('/api/files/mkdir', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path })
        })
      )
      newFolderName = ''
      await browse(path)
    } catch (e) {
      issue = e.message
    } finally {
      busy = false
    }
  }
  onMount(() => browse(folder))
  onDestroy(() => controller?.abort())
  async function browse(path, nextPage = 1) {
    controller?.abort()
    const request = new AbortController()
    controller = request
    loading = true
    issue = ''
    try {
      const query = new URLSearchParams({
        path,
        page: nextPage,
        per_page: 100,
        sort_by: 'name',
        sort_asc: true
      })
      const data = await (
        await requireOK(
          await apiFetch(`/api/files?${query}`, { signal: request.signal })
        )
      ).json()
      if (request.signal.aborted) return
      folder = path
      folderInput = path
      page = data.page
      totalPages = Math.max(1, Math.ceil(data.total / data.per_page))
      entries = (data.files || []).filter(
        (f) =>
          f.is_dir &&
          !items.some((src) => f.path === src || f.path.startsWith(src + '/'))
      )
    } catch (e) {
      if (!request.signal.aborted) issue = e.message
    } finally {
      if (!request.signal.aborted) loading = false
    }
  }
  async function submit() {
    if (busy || loading) return
    busy = true
    issue = ''
    const results = await transferFiles(items, folder, mode, name)
    const failed = results.filter((r) => !r.ok)
    await onComplete(results)
    busy = false
    if (!failed.length) onClose()
    else {
      items = failed.map((r) => r.path)
      issue = failed.map((r) => `${r.path}: ${r.error}`).join('\n')
    }
  }
</script>

<Dialog
  title={mode === 'copy' ? $i18n('Copy items') : $i18n('Move items')}
  {busy}
  {onClose}
>
  <p class="dialog-hint">
    {items.length}
    {$i18n('items selected. Existing files will not be overwritten.')}
  </p>
  <Breadcrumb path={folder} onNavigate={(path) => !busy && browse(path)} />
  <form
    onsubmit={(e) => {
      e.preventDefault()
      browse(folderInput.replace(/^\/+|\/+$/g, ''))
    }}
    class="path-form"
  >
    <label class="field"
      >{$i18n('Destination folder')}<input
        bind:value={folderInput}
        disabled={busy}
        placeholder={$i18n('Folder path (empty = Home)')}
      /></label
    >
    <button disabled={busy || loading}>{$i18n('Go')}</button>
  </form>
  <div class="folders" aria-busy={loading}>
    {#if loading}<p class="dialog-hint">{$i18n('Loading folders…')}</p>
    {:else}{#each entries as entry}<button
          disabled={busy}
          onclick={() => browse(entry.path)}
          ><Icon name="folder" size={18} />{entry.name}<Icon
            name="chevron_r"
            size={16}
          /></button
        >
      {:else}<p class="dialog-hint">
          {$i18n('No subfolders on this page')}
        </p>{/each}{/if}
  </div>
  {#if totalPages > 1}<div class="pages">
      <button
        disabled={page <= 1 || busy || loading}
        onclick={() => browse(folder, page - 1)}>{$i18n('Previous')}</button
      ><span>{page} / {totalPages}</span><button
        disabled={page >= totalPages || busy || loading}
        onclick={() => browse(folder, page + 1)}>{$i18n('Next')}</button
      >
    </div>{/if}
  {#if items.length === 1}<label class="field"
      >{$i18n('Destination name')}<input
        bind:value={name}
        disabled={busy}
      /></label
    >{/if}
  <form
    class="path-form"
    onsubmit={(e) => {
      e.preventDefault()
      createFolder()
    }}
  >
    <label class="field"
      >{$i18n('New destination folder')}<input
        bind:value={newFolderName}
        disabled={busy}
        placeholder={$i18n('Folder name')}
      /></label
    ><button disabled={busy || loading || !newFolderName.trim()}
      >{$i18n('Create')}</button
    >
  </form>
  {#if issue}<p class="dialog-error" role="alert">{issue}</p>{/if}
  <div class="dialog-footer">
    <button disabled={busy} onclick={onClose}>{$i18n('Cancel')}</button><button
      class="dialog-primary"
      disabled={busy || loading}
      onclick={submit}
      >{busy
        ? $i18n('Working…')
        : mode === 'copy'
          ? $i18n('Copy here')
          : $i18n('Move here')}</button
    >
  </div>
</Dialog>

<style>
  .path-form {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .path-form label {
    flex: 1;
    min-width: 0;
  }
  .folders {
    border: 1px solid var(--border);
    border-radius: 8px;
    max-height: 220px;
    overflow: auto;
    padding: 8px;
  }
  .folders button {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    border: 0;
    background: none;
    text-align: start;
  }
  .folders button:hover {
    background: var(--row-hover);
  }
  .folders button :global(svg:last-child) {
    margin-left: auto;
  }
  .pages {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-top: 10px;
    font-size: 12px;
  }
</style>
