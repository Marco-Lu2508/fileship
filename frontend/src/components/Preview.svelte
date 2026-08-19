<script>
  import { downloadUrl } from '../stores/files.js'
  import Icon from './Icon.svelte'

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
    fetch(url).then(r => r.text()).then(t => textContent = t).catch(() => textContent = 'Could not load file.')
  }

  function handleKey(e) { if (e.key === 'Escape') onClose() }
</script>

<svelte:window onkeydown={handleKey} />

{#if file}
  <div class="overlay" role="dialog" aria-modal="true" aria-label="Preview" tabindex="-1" onclick={(e) => { if (e.target === e.currentTarget) onClose() }} onkeydown={handleKey}>
    <div class="modal">
      <div class="modal-header">
        <span class="filename">{file.name}</span>
        <div class="modal-actions">
          <a href={url} download={file.name} class="btn">
            <Icon name="download" size={13} /> Download
          </a>
          <button class="close-btn" onclick={onClose}><Icon name="x" size={14} /></button>
        </div>
      </div>
      <div class="modal-body">
        {#if isImage}
          <img src={url} alt={file.name} />
        {:else if isVideo}
          <video controls src={url}><track kind="captions" /></video>
        {:else if isAudio}
          <audio controls src={url}></audio>
        {:else if isPdf}
          <iframe src={url} title={file.name}></iframe>
        {:else if isText}
          <pre>{textContent}</pre>
        {:else}
          <div class="unsupported">
            <Icon name="file" size={36} />
            <p>No preview available</p>
            <a href={url} download={file.name} class="btn primary">
              <Icon name="download" size={13} /> Download
            </a>
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed; inset: 0; background: rgba(0,0,0,0.8);
    display: flex; align-items: center; justify-content: center;
    z-index: 500; padding: 1rem;
  }
  .modal {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: 6px; width: 100%; max-width: 900px; max-height: 90vh;
    display: flex; flex-direction: column; overflow: hidden;
    box-shadow: var(--shadow);
  }
  .modal-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 0.75rem 1rem; border-bottom: 1px solid var(--border); gap: 1rem;
  }
  .filename { font-size: 0.875rem; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .modal-actions { display: flex; gap: 0.4rem; align-items: center; flex-shrink: 0; }
  .btn {
    display: inline-flex; align-items: center; gap: 0.3rem;
    background: var(--bg2); border: 1px solid var(--border); color: var(--text);
    padding: 0.3rem 0.65rem; border-radius: 3px; cursor: pointer; font-size: 0.8rem; text-decoration: none;
  }
  .btn:hover { background: var(--row-hover); }
  .btn.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
  .btn.primary:hover { background: var(--accent-h); }
  .close-btn { background: none; border: none; color: var(--text2); cursor: pointer; padding: 0.3rem; border-radius: 3px; display: flex; }
  .close-btn:hover { background: var(--border); color: var(--text); }
  .modal-body { flex: 1; overflow: auto; display: flex; align-items: center; justify-content: center; padding: 1rem; min-height: 200px; }
  img { max-width: 100%; max-height: 75vh; object-fit: contain; }
  video { max-width: 100%; max-height: 75vh; }
  audio { width: 100%; }
  iframe { width: 100%; height: 75vh; border: none; }
  pre { white-space: pre-wrap; word-break: break-all; font-size: 0.82rem; color: var(--text2); font-family: monospace; width: 100%; max-height: 75vh; overflow: auto; }
  .unsupported { text-align: center; color: var(--text2); display: flex; flex-direction: column; align-items: center; gap: 1rem; }
  .unsupported p { font-size: 0.875rem; }
</style>
