<script>
  import { loadFiles, currentPath } from '../stores/files.js'
  import { apiFetch } from '../stores/auth.js'
  import { csrfHeaders } from '../lib/csrf.js'
  import { success, error } from '../stores/toast.js'
  import { get } from 'svelte/store'
  import Icon from './Icon.svelte'

  let dragging = false
  let uploads = []
  let folderInput

  async function handleDrop(e) {
    e.preventDefault()
    dragging = false
    await upload(e.dataTransfer.files)
  }

  async function handleInput(e) {
    await upload(e.target.files)
    e.target.value = ''
  }

  function openFolderPicker() { folderInput?.click() }

  async function upload(fileList) {
    if (!fileList.length) return
    const path = get(currentPath)
    const token = localStorage.getItem('access_token')

    for (const file of fileList) {
      const entry = { name: file.name, progress: 0 }
      uploads = [...uploads, entry]

      await new Promise((resolve) => {
        const xhr = new XMLHttpRequest()
        const form = new FormData()
        form.append('path', path)
        form.append('files', file, file.webkitRelativePath || file.name)

        xhr.upload.onprogress = (e) => {
          if (e.lengthComputable) {
            entry.progress = Math.round((e.loaded / e.total) * 100)
            uploads = [...uploads]
          }
        }

        xhr.onload = () => {
          uploads = uploads.filter(u => u !== entry)
          xhr.status === 201 ? success(`Uploaded "${file.name}"`) : error(`Failed: "${file.name}"`)
          resolve()
        }
        xhr.onerror = () => { uploads = uploads.filter(u => u !== entry); error(`Failed: "${file.name}"`); resolve() }

        xhr.open('POST', '/api/files/upload')
        xhr.setRequestHeader('Authorization', `Bearer ${token}`)
        xhr.setRequestHeader('X-CSRF-Token', csrfHeaders()['X-CSRF-Token'])
        xhr.send(form)
      })
    }
    await loadFiles(path)
  }
</script>

<div
  class="dropzone"
  class:active={dragging}
  ondragover={(e) => { e.preventDefault(); dragging = true }}
  ondragleave={() => dragging = false}
  ondrop={handleDrop}
  role="region"
  aria-label="Upload area"
>
  <label class="drop-label">
    <Icon name="upload" size={16} />
    <span>Drop files here or <span class="link">browse</span></span>
    <input type="file" multiple onchange={handleInput} hidden />
    <button type="button" class="folder-button" onclick={openFolderPicker} title="Upload folder"><Icon name="folder" size={14} /></button>
    <input bind:this={folderInput} type="file" webkitdirectory directory multiple onchange={handleInput} hidden />
  </label>
</div>

{#if uploads.length > 0}
  <div class="upload-list">
    {#each uploads as u}
      <div class="upload-item">
        <span class="upload-name">{u.name}</span>
        <div class="progress-bar"><div class="progress-fill" style="width:{u.progress}%"></div></div>
        <span class="upload-pct">{u.progress}%</span>
      </div>
    {/each}
  </div>
{/if}

<style>
  .dropzone {
    border: 1px dashed var(--border); border-radius: 4px;
    padding: 0.75rem 1rem; color: var(--text2);
    transition: border-color 0.15s, background 0.15s;
    margin-bottom: 0.75rem;
  }
  .dropzone.active { border-color: var(--accent); background: var(--row-hover); color: var(--text); }
  .dropzone:hover { border-color: var(--text2); }
  .drop-label { display: flex; align-items: center; gap: 0.5rem; cursor: pointer; font-size: 0.85rem; }
  .link { color: var(--accent); text-decoration: underline; }
  .folder-button { margin-left: auto; display: flex; align-items: center; border: 1px solid var(--border); background: var(--surface); color: var(--text2); border-radius: 4px; padding: 0.35rem; cursor: pointer; }
  .folder-button:hover { color: var(--accent); border-color: var(--accent); }
  .upload-list { display: flex; flex-direction: column; gap: 0.3rem; margin-bottom: 0.75rem; }
  .upload-item { display: flex; align-items: center; gap: 0.75rem; background: var(--surface); border-radius: 3px; padding: 0.4rem 0.75rem; border: 1px solid var(--border); }
  .upload-name { font-size: 0.82rem; color: var(--text2); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .progress-bar { flex: 2; height: 4px; background: var(--border); border-radius: 2px; overflow: hidden; }
  .progress-fill { height: 100%; background: var(--accent); border-radius: 2px; transition: width 0.1s; }
  .upload-pct { font-size: 0.78rem; color: var(--text2); width: 3rem; text-align: right; }
</style>
