<script>
  import { loadFiles, currentPath } from '../stores/files.js'
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import { get } from 'svelte/store'

  let dragging = false
  let uploads = []  // { name, progress }

  async function handleDrop(e) {
    e.preventDefault()
    dragging = false
    await upload(e.dataTransfer.files)
  }

  async function handleInput(e) {
    await upload(e.target.files)
    e.target.value = ''
  }

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
        form.append('files', file)

        xhr.upload.onprogress = (e) => {
          if (e.lengthComputable) {
            entry.progress = Math.round((e.loaded / e.total) * 100)
            uploads = [...uploads]
          }
        }

        xhr.onload = () => {
          uploads = uploads.filter(u => u !== entry)
          if (xhr.status === 201) {
            success(`Uploaded "${file.name}"`)
          } else {
            error(`Failed to upload "${file.name}"`)
          }
          resolve()
        }

        xhr.onerror = () => {
          uploads = uploads.filter(u => u !== entry)
          error(`Failed to upload "${file.name}"`)
          resolve()
        }

        xhr.open('POST', '/api/files/upload')
        xhr.setRequestHeader('Authorization', `Bearer ${token}`)
        xhr.send(form)
      })
    }

    await loadFiles(path)
  }
</script>

<div
  class="dropzone"
  class:dragging
  ondragover={(e) => { e.preventDefault(); dragging = true }}
  ondragleave={() => dragging = false}
  ondrop={handleDrop}
  role="region"
  aria-label="Upload area"
>
  <label>
    📁 Drop files here or <span class="link">browse</span>
    <input type="file" multiple onchange={handleInput} hidden />
  </label>
</div>

{#if uploads.length > 0}
  <div class="upload-list">
    {#each uploads as u}
      <div class="upload-item">
        <span class="upload-name">{u.name}</span>
        <div class="progress-bar">
          <div class="progress-fill" style="width: {u.progress}%"></div>
        </div>
        <span class="upload-pct">{u.progress}%</span>
      </div>
    {/each}
  </div>
{/if}

<style>
  .dropzone {
    border: 2px dashed #2a2d3a; border-radius: 8px; padding: 1.25rem;
    text-align: center; color: #718096; transition: all 0.2s; cursor: pointer;
  }
  .dropzone.dragging { border-color: #5865f2; background: #1e2035; color: #fff; }
  .dropzone:hover { border-color: #4a5568; }
  label { cursor: pointer; }
  .link { color: #5865f2; text-decoration: underline; }
  .upload-list { margin-top: 0.75rem; display: flex; flex-direction: column; gap: 0.4rem; }
  .upload-item { display: flex; align-items: center; gap: 0.75rem; background: #1a1d27; border-radius: 6px; padding: 0.5rem 0.75rem; }
  .upload-name { font-size: 0.85rem; color: #a0aec0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .progress-bar { flex: 2; height: 6px; background: #2a2d3a; border-radius: 3px; overflow: hidden; }
  .progress-fill { height: 100%; background: #5865f2; border-radius: 3px; transition: width 0.1s; }
  .upload-pct { font-size: 0.8rem; color: #718096; width: 3rem; text-align: right; }
</style>
