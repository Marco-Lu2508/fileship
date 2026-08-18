<script>
  import { downloadUrl } from '../stores/files.js'

  export let file = null
  export let onClose = () => {}

  $: url = file ? downloadUrl(file.path) : ''
  $: mime = file?.mime_type || ''
  $: isImage = mime.startsWith('image/')
  $: isVideo = mime.startsWith('video/')
  $: isAudio = mime.startsWith('audio/')
  $: isPdf   = mime.includes('pdf')
  $: isText  = mime.startsWith('text/')

  let textContent = ''

  $: if (isText && file) {
    fetch(url)
      .then(r => r.text())
      .then(t => textContent = t)
      .catch(() => textContent = 'Could not load file.')
  }

  function handleKey(e) {
    if (e.key === 'Escape') onClose()
  }
</script>

<svelte:window onkeydown={handleKey} />

{#if file}
  <div class="overlay" onclick={onClose} role="dialog" aria-modal="true">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <div class="modal-header">
        <span class="filename">{file.name}</span>
        <div class="modal-actions">
          <a href={url} download={file.name} class="btn">⬇️ Download</a>
          <button class="close" onclick={onClose}>✕</button>
        </div>
      </div>
      <div class="modal-body">
        {#if isImage}
          <img src={url} alt={file.name} />
        {:else if isVideo}
          <video controls src={url}>
            <track kind="captions" />
          </video>
        {:else if isAudio}
          <audio controls src={url}></audio>
        {:else if isPdf}
          <iframe src={url} title={file.name}></iframe>
        {:else if isText}
          <pre>{textContent}</pre>
        {:else}
          <div class="unsupported">
            <p>👁️ No preview available</p>
            <a href={url} download={file.name} class="btn-dl">⬇️ Download file</a>
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed; inset: 0;
    background: rgba(0,0,0,0.85);
    display: flex; align-items: center; justify-content: center;
    z-index: 500; padding: 1rem;
  }
  .modal {
    background: #1a1d27;
    border: 1px solid #2a2d3a;
    border-radius: 12px;
    width: 100%; max-width: 900px;
    max-height: 90vh;
    display: flex; flex-direction: column;
    overflow: hidden;
  }
  .modal-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 1rem 1.25rem;
    border-bottom: 1px solid #2a2d3a;
    gap: 1rem;
  }
  .filename { font-size: 0.95rem; color: #e2e8f0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .modal-actions { display: flex; gap: 0.5rem; align-items: center; flex-shrink: 0; }
  .btn { background: #2a2d3a; border: none; color: #e2e8f0; padding: 0.35rem 0.75rem; border-radius: 6px; cursor: pointer; font-size: 0.85rem; text-decoration: none; }
  .btn:hover { background: #3a3d4a; }
  .close { background: none; border: none; color: #718096; cursor: pointer; font-size: 1.1rem; padding: 0.25rem 0.5rem; border-radius: 4px; }
  .close:hover { background: #2a2d3a; color: #fff; }
  .modal-body { flex: 1; overflow: auto; display: flex; align-items: center; justify-content: center; padding: 1rem; min-height: 200px; }
  img { max-width: 100%; max-height: 75vh; object-fit: contain; border-radius: 4px; }
  video { max-width: 100%; max-height: 75vh; }
  audio { width: 100%; }
  iframe { width: 100%; height: 75vh; border: none; }
  pre { white-space: pre-wrap; word-break: break-all; font-size: 0.85rem; color: #a0aec0; font-family: monospace; width: 100%; max-height: 75vh; overflow: auto; }
  .unsupported { text-align: center; color: #718096; }
  .btn-dl { display: inline-block; margin-top: 1rem; background: #5865f2; color: #fff; padding: 0.6rem 1.25rem; border-radius: 8px; text-decoration: none; }
</style>
