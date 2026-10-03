<script>
  import { locale, translator as i18n } from '../stores/i18n.js'
  import { onDestroy } from 'svelte'
  import { get } from 'svelte/store'
  import { loadFiles, currentPath } from '../stores/files.js'
  import { accessToken, refreshTokens } from '../stores/auth.js'
  import { csrfHeaders, ensureCSRFToken } from '../lib/csrf.js'
  import { success, error } from '../stores/toast.js'
  import { formatBytes } from '../lib/files.js'
  import Icon from './Icon.svelte'

  let dragging = false,
    uploads = [],
    folderInput,
    fileInput,
    active = true
  const requests = new Set()
  export let expanded = true
  export function openPicker() {
    fileInput?.click()
  }
  export function openFolderPicker() {
    folderInput?.click()
  }
  onDestroy(() => {
    active = false
    requests.forEach((xhr) => xhr.abort())
  })

  function send(entry, path) {
    return new Promise((resolve) => {
      const xhr = new XMLHttpRequest(),
        form = new FormData()
      requests.add(xhr)
      entry.xhr = xhr
      form.append('path', path)
      form.append(
        'files',
        entry.file,
        entry.file.webkitRelativePath || entry.name
      )
      if (entry.file.webkitRelativePath)
        form.append('relative_path', entry.file.webkitRelativePath)
      xhr.upload.onprogress = (event) => {
        if (event.lengthComputable) {
          entry.progress = Math.round((event.loaded / event.total) * 100)
          uploads = [...uploads]
        }
      }
      xhr.onload = () => {
        requests.delete(xhr)
        resolve({
          status: xhr.status,
          message: xhr.responseText.trim().slice(0, 160)
        })
      }
      xhr.onerror = () => {
        requests.delete(xhr)
        resolve({ status: 0, message: $i18n('Connection interrupted') })
      }
      xhr.onabort = () => {
        requests.delete(xhr)
        resolve({ status: 0, message: $i18n('Upload cancelled') })
      }
      xhr.ontimeout = () => {
        requests.delete(xhr)
        resolve({ status: 0, message: $i18n('Upload timed out') })
      }
      xhr.open('POST', '/api/files/upload')
      xhr.timeout = 30 * 60 * 1000
      xhr.setRequestHeader('Authorization', `Bearer ${get(accessToken)}`)
      xhr.setRequestHeader('X-CSRF-Token', csrfHeaders()['X-CSRF-Token'])
      xhr.send(form)
    })
  }
  export async function upload(fileList, destination = get(currentPath)) {
    const batch = Array.from(fileList || []).map((file) => ({
      file,
      name: file.name,
      path: destination,
      progress: 0,
      state: 'queued',
      message: ''
    }))
    if (!batch.length) return
    uploads = [...uploads, ...batch]
    let completed = 0
    for (const entry of batch) {
      if (!active) break
      entry.state = 'uploading'
      uploads = [...uploads]
      try {
        await ensureCSRFToken()
        let result = await send(entry, entry.path)
        if (result.status === 401) {
          await refreshTokens()
          if (active) result = await send(entry, entry.path)
        }
        if (result.status < 200 || result.status >= 300)
          throw new Error(result.message || $i18n('Upload failed'))
        entry.state = 'done'
        entry.progress = 100
        completed++
      } catch (err) {
        entry.state = 'failed'
        entry.message = err.message
      }
      uploads = [...uploads]
    }
    if (!active) return
    if (completed)
      success($i18n('{count} files uploaded', { count: completed }))
    if (batch.some((entry) => entry.state === 'failed'))
      error($i18n('Some uploads failed. See details below.'))
    // A finished upload must never navigate the user back to its destination.
    await loadFiles(get(currentPath))
  }
  async function handleInput(event) {
    const list = Array.from(event.target.files || [])
    event.target.value = ''
    await upload(list)
  }
  function handleDrop(event) {
    if (!Array.from(event.dataTransfer?.types || []).includes($i18n('Files')))
      return
    event.preventDefault()
    dragging = false
    upload(event.dataTransfer.files)
  }
  $: busy = uploads.some(
    (entry) => entry.state === 'queued' || entry.state === 'uploading'
  )
</script>

<div
  class="dropzone"
  class:hidden={!expanded}
  class:active={dragging}
  role="region"
  aria-label={$i18n('Upload area')}
  ondragover={(e) => {
    if (Array.from(e.dataTransfer?.types || []).includes($i18n('Files'))) {
      e.preventDefault()
      dragging = true
    }
  }}
  ondragleave={() => (dragging = false)}
  ondrop={handleDrop}
>
  <span class="upload-icon"><Icon name="upload" size={20} /></span>
  <div class="drop-copy">
    <strong
      >{dragging
        ? $i18n('Drop files to upload')
        : $i18n('Upload to this folder')}</strong
    >
    <p>
      {$i18n('Drag & drop your files here, or')}
      <button onclick={openPicker}>{$i18n('browse files')}</button>
    </p>
  </div>
  <button class="folder-button" onclick={openFolderPicker}
    ><Icon name="folder" size={16} /><span>{$i18n('Upload folder')}</span
    ></button
  >
  <input
    bind:this={fileInput}
    type="file"
    multiple
    onchange={handleInput}
    hidden
    aria-label={$i18n('Upload files')}
  /><input
    bind:this={folderInput}
    type="file"
    webkitdirectory
    directory
    multiple
    onchange={handleInput}
    hidden
    aria-label={$i18n('Upload folder')}
  />
</div>
{#if uploads.length}<section
    class="upload-list"
    aria-label={$i18n('Upload progress')}
  >
    <div class="upload-heading">
      <strong
        >{busy
          ? $i18n('Uploading to your workspace')
          : $i18n('Upload summary')}</strong
      ><button
        onclick={() =>
          (uploads = uploads.filter(
            (entry) => entry.state === 'uploading' || entry.state === 'queued'
          ))}>{$i18n('Clear completed')}</button
      >
    </div>
    {#each uploads as entry}<div class="upload-item">
        <Icon
          name={entry.state === 'done'
            ? 'check'
            : entry.state === 'failed'
              ? 'warning'
              : 'file'}
          size={16}
        />
        <div class="upload-details">
          <strong title={entry.name}>{entry.name}</strong><span
            >{entry.state === 'failed'
              ? entry.message
              : `${formatBytes(entry.file.size, $locale)} · ${entry.state === 'queued' ? $i18n('Queued') : entry.state === 'done' ? $i18n('Complete') : `${entry.progress}%`}`}</span
          >
        </div>
        {#if entry.state === 'uploading'}<progress
            value={entry.progress}
            max="100"
            aria-label={$i18n('Uploading {name}', { name: entry.name })}
          ></progress>{/if}{#if entry.state === 'failed'}<button
            onclick={() => {
              uploads = uploads.filter((item) => item !== entry)
              upload([entry.file], entry.path)
            }}>{$i18n('Retry')}</button
          >{/if}
      </div>{/each}
  </section>{/if}

<style>
  .dropzone.hidden {
    display: none;
  }
  .dropzone {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    border: 1px dashed var(--upload-border);
    border-radius: 10px;
    background: var(--upload-bg);
    transition:
      background 0.15s,
      border-color 0.15s;
  }
  .dropzone.active {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .upload-icon {
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    border-radius: 9px;
    background: var(--surface);
    color: var(--accent);
    box-shadow: 0 2px 5px #182c1410;
    flex-shrink: 0;
  }
  .drop-copy strong {
    font-size: 13px;
    font-weight: 600;
    color: var(--text);
  }
  .drop-copy p {
    color: var(--text2);
    font-size: 11px;
    margin-top: 5px;
  }
  .drop-copy button {
    border: 0;
    background: none;
    color: var(--accent);
    font-size: inherit;
    font-weight: 600;
    cursor: pointer;
    text-decoration: underline;
    text-underline-offset: 3px;
  }
  .folder-button {
    display: flex;
    align-items: center;
    gap: 8px;
    border: 1px solid var(--border);
    background: var(--surface);
    color: var(--text2);
    border-radius: 7px;
    padding: 10px 13px;
    font-size: 11px;
    cursor: pointer;
    margin-left: auto;
    white-space: nowrap;
  }
  .folder-button:hover {
    border-color: var(--accent);
    color: var(--accent);
  }
  .upload-list {
    border: 1px solid var(--border);
    border-radius: 9px;
    padding: 12px 16px;
    background: var(--surface);
    margin-top: 12px;
    max-height: 240px;
    overflow: auto;
  }
  .upload-heading {
    display: flex;
    justify-content: space-between;
    gap: 10px;
    font-size: 12px;
    margin-bottom: 7px;
  }
  .upload-heading button,
  .upload-item button {
    background: none;
    border: 0;
    cursor: pointer;
    color: var(--accent);
    font-size: 11px;
  }
  .upload-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 0;
    color: var(--text2);
  }
  .upload-details {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }
  .upload-details strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11px;
    font-weight: 500;
  }
  .upload-details span {
    font-size: 10px;
  }
  progress {
    width: 100px;
    height: 6px;
    accent-color: var(--accent);
  }
  @media (max-width: 600px) {
    .dropzone {
      padding: 12px 14px;
      gap: 12px;
      flex-wrap: wrap;
    }
    .folder-button {
      margin: 0 0 0 50px;
    }
    .upload-icon {
      width: 38px;
      height: 38px;
    }
    .drop-copy {
      flex: 1 1 calc(100% - 50px);
    }
    .drop-copy strong {
      font-size: 12px;
    }
  }
</style>
