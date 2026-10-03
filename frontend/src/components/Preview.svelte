<script>
  import Modal from './Modal.svelte'
  import { translator as i18n } from '../stores/i18n.js'
  import { apiFetch } from '../stores/auth.js'
  import Icon from './Icon.svelte'
  import { onDestroy } from 'svelte'
  import { renderMarkdown } from '../lib/markdown.js'

  export let file = null
  export let onClose = () => {}
  export let siblings = [],
    onNavigate = () => {}
  let fullSize = false,
    previousPath = ''
  let rawMarkdown = false
  $: markdown =
    extension === 'md' && !rawMarkdown ? renderMarkdown(textContent) : ''
  $: position = siblings.findIndex((f) => f.path === file?.path)
  $: if (file?.path !== previousPath) {
    previousPath = file?.path || ''
    fullSize = false
  }
  function navigate(offset) {
    if (position + offset >= 0 && position + offset < siblings.length)
      onNavigate(siblings[position + offset])
  }

  let url = ''
  let previewRequest = 0
  let loading = false
  let loadError = false
  $: mime = file?.mime_type || ''
  $: extension = file?.name?.split('.').pop()?.toLowerCase() || ''
  $: isImage =
    mime.startsWith('image/') ||
    ['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'svg'].includes(extension)
  $: isVideo =
    mime.startsWith('video/') ||
    ['mp4', 'webm', 'ogv', 'mov', 'm4v'].includes(extension)
  $: isAudio =
    mime.startsWith('audio/') ||
    ['mp3', 'wav', 'ogg', 'oga', 'm4a', 'flac'].includes(extension)
  $: isPdf = mime.includes('pdf') || extension === 'pdf'
  $: isText =
    mime.startsWith('text/') ||
    [
      'txt',
      'md',
      'json',
      'yaml',
      'yml',
      'csv',
      'log',
      'xml',
      'html',
      'css',
      'js',
      'ts',
      'go',
      'py',
      'sh'
    ].includes(extension)

  let textContent = ''
  $: previewUrl = file
    ? `/api/files/preview?path=${encodeURIComponent(file.path)}&access_token=${encodeURIComponent(localStorage.getItem('access_token') || '')}`
    : ''
  $: if (file) loadPreview(file)

  async function loadPreview(currentFile) {
    const request = ++previewRequest
    loading = true
    loadError = false
    if (url) URL.revokeObjectURL(url)
    url = ''
    textContent = ''
    if (!isText) {
      url = previewUrl
      loading = false
      return
    }
    try {
      const res = await apiFetch(
        `/api/files/download?path=${encodeURIComponent(currentFile.path)}`
      )
      if (!res.ok) throw new Error($i18n('preview failed'))
      const blob = await res.blob()
      if (request !== previewRequest) return
      if (isText) {
        textContent = await blob.text()
        url = URL.createObjectURL(blob)
      } else {
        url = previewUrl
      }
      loading = false
    } catch {
      if (request === previewRequest) {
        loading = false
        loadError = true
        if (isText) textContent = $i18n('Could not load file.')
      }
    }
  }

  onDestroy(() => {
    if (url) URL.revokeObjectURL(url)
  })

  function handleKey(e) {
    if (!file || e.target.closest('input,textarea,select')) return
    if (e.key === 'ArrowLeft') {
      e.preventDefault()
      navigate(-1)
    }
    if (e.key === 'ArrowRight') {
      e.preventDefault()
      navigate(1)
    }
  }
</script>

<svelte:window onkeydown={handleKey} />

{#if file}
  <Modal label={$i18n('Preview')} width="900px" {onClose}>
    <div class="modal">
      <div class="modal-header">
        <span class="filename">{file.name}</span>
        <div class="modal-actions">
          {#if siblings.length > 1}<button
              class="btn"
              aria-label={$i18n('Previous preview')}
              disabled={position <= 0}
              onclick={() => navigate(-1)}
              ><Icon name="chevron_l" size={16} /></button
            ><span class="preview-position"
              >{position + 1} / {siblings.length}</span
            ><button
              class="btn"
              aria-label={$i18n('Next preview')}
              disabled={position >= siblings.length - 1}
              onclick={() => navigate(1)}
              ><Icon name="chevron_r" size={16} /></button
            >{/if}
          {#if isImage}<button
              class="btn"
              onclick={() => (fullSize = !fullSize)}
              >{fullSize ? $i18n('Fit image') : $i18n('Original size')}</button
            >{/if}
          {#if extension === 'md'}<button
              class="btn"
              onclick={() => (rawMarkdown = !rawMarkdown)}
              >{rawMarkdown
                ? $i18n('Formatted preview')
                : $i18n('Source text')}</button
            >{/if}
          <a href={url} download={file.name} class="btn">
            <Icon name="download" size={13} />
            {$i18n('Download')}
          </a>
          <button
            class="close-btn"
            aria-label={$i18n('Close preview')}
            onclick={onClose}><Icon name="x" size={14} /></button
          >
        </div>
      </div>
      <div class="modal-body">
        {#if loading}
          <div class="preview-state">
            <span class="spinner"></span>
            <p>{$i18n('Loading preview...')}</p>
          </div>
        {:else if loadError}
          <div class="preview-state">
            <Icon name="warning" size={32} />
            <p>{$i18n('Preview could not be loaded.')}</p>
          </div>
        {:else if isImage}
          <img
            class:full-size={fullSize}
            src={url}
            alt={file.name}
            onload={() => (loading = false)}
            onerror={() => (loadError = true)}
          />
        {:else if isVideo}
          <div class="video-stage">
            <video
              controls
              playsinline
              preload="metadata"
              src={previewUrl}
              oncanplay={() => (loading = false)}
              onerror={() => (loadError = true)}
              ><track kind="captions" /></video
            >
          </div>
        {:else if isAudio}
          <audio
            controls
            src={previewUrl}
            oncanplay={() => (loading = false)}
            onerror={() => (loadError = true)}
          ></audio>
        {:else if isPdf}
          <iframe
            src={previewUrl}
            title={file.name}
            onload={() => (loading = false)}
          ></iframe>
        {:else if isText}
          {#if extension === 'md' && !rawMarkdown}<article class="markdown">
              {@html markdown}
            </article>{:else}<pre>{textContent}</pre>{/if}
        {:else}
          <div class="unsupported">
            <Icon name="file" size={36} />
            <p>{$i18n('No preview available')}</p>
            <a href={url} download={file.name} class="btn primary">
              <Icon name="download" size={13} />
              {$i18n('Download')}
            </a>
          </div>
        {/if}
      </div>
    </div>
  </Modal>
{/if}

<style>
  .markdown {
    width: 100%;
    line-height: 1.7;
    color: var(--text);
    font-size: 14px;
    padding: 15px;
  }
  .markdown :global(h1),
  .markdown :global(h2),
  .markdown :global(h3) {
    margin: 20px 0 12px;
  }
  .markdown :global(p),
  .markdown :global(ul),
  .markdown :global(ol),
  .markdown :global(pre) {
    margin: 12px 0;
  }
  .markdown :global(ul),
  .markdown :global(ol) {
    padding-left: 25px;
  }
  .markdown :global(pre) {
    padding: 14px;
    background: var(--surface2);
    border-radius: 6px;
  }
  .markdown :global(a) {
    color: var(--accent);
  }
  .markdown :global(table) {
    border-collapse: collapse;
    width: 100%;
  }
  .markdown :global(th),
  .markdown :global(td) {
    padding: 7px;
    border: 1px solid var(--border);
  }
  .preview-position {
    font-size: 11px;
    color: var(--text2);
    white-space: nowrap;
  }
  img.full-size {
    max-width: none;
    max-height: none;
  }
  @media (max-width: 600px) {
    .modal-header {
      flex-wrap: wrap;
    }
    .modal-actions {
      flex-wrap: wrap;
    }
  }
  .modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 6px;
    width: 100%;
    max-width: 900px;
    max-height: calc(100dvh - 32px);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    box-shadow: var(--shadow);
  }
  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border);
    gap: 1rem;
    flex-wrap: wrap;
  }
  .filename {
    font-size: 0.875rem;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .modal-actions {
    flex-wrap: wrap;
    display: flex;
    gap: 0.4rem;
    align-items: center;
    flex-shrink: 0;
  }
  .btn {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    background: var(--bg2);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 0.3rem 0.65rem;
    border-radius: 3px;
    cursor: pointer;
    font-size: 0.8rem;
    text-decoration: none;
  }
  .btn:hover {
    background: var(--row-hover);
  }
  .btn.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--on-accent);
  }
  .btn.primary:hover {
    background: var(--accent-h);
  }
  .close-btn {
    background: none;
    border: none;
    color: var(--text2);
    cursor: pointer;
    padding: 0.3rem;
    border-radius: 3px;
    display: flex;
  }
  .close-btn:hover {
    background: var(--border);
    color: var(--text);
  }
  .modal-body {
    overscroll-behavior: contain;
    flex: 1;
    overflow: auto;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
    min-height: 200px;
  }
  img {
    max-width: 100%;
    max-height: 75vh;
    object-fit: contain;
  }
  .video-stage {
    width: min(100%, 860px);
    aspect-ratio: 16 / 9;
    display: grid;
    place-items: center;
    background: #080b10;
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
  }
  video {
    width: 100%;
    height: 100%;
    object-fit: contain;
    background: #080b10;
  }
  audio {
    width: 100%;
  }
  iframe {
    width: 100%;
    height: 75vh;
    border: none;
  }
  pre {
    white-space: pre-wrap;
    word-break: break-all;
    font-size: 0.82rem;
    color: var(--text2);
    font-family: monospace;
    width: 100%;
    max-height: 75vh;
    overflow: auto;
  }
  .unsupported {
    text-align: center;
    color: var(--text2);
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1rem;
  }
  .unsupported p {
    font-size: 0.875rem;
  }
  .preview-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.75rem;
    color: var(--text2);
  }
  .spinner {
    width: 28px;
    height: 28px;
    border: 3px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
