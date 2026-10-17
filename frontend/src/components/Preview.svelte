<script>
  import Modal from './Modal.svelte'
  import { translator as i18n } from '../stores/i18n.js'
  import {
    accessToken,
    apiFetch,
    activeSourceId,
    activeAgentId
  } from '../stores/auth.js'
  import { getSourceParam } from '../stores/sources.js'
  import Icon from './Icon.svelte'
  import { onDestroy } from 'svelte'
  import { renderMarkdown } from '../lib/markdown.js'
  import { fileKind } from '../lib/files.js'

  export let file = null
  export let onClose = () => {}
  export let siblings = [],
    onNavigate = () => {}
  let fullSize = false,
    theaterMode = false,
    previousPath = ''
  let rawMarkdown = false
  let playbackRate = 1
  let loopMedia = false
  let autoNext = true
  let mediaPaused = true
  let currentTime = 0
  let duration = 0
  let videoEl = null
  let audioEl = null
  let visualizerCanvas = null
  let animFrame = null
  let volume = 1
  let isMuted = false

  const speeds = [0.5, 0.75, 1, 1.25, 1.5, 2]

  $: markdown =
    extension === 'md' && !rawMarkdown ? renderMarkdown(textContent) : ''
  $: position = siblings.findIndex((f) => f.path === file?.path)
  $: audioSiblings = siblings.filter((s) => fileKind(s) === 'audio')
  $: if (file?.path !== previousPath) {
    previousPath = file?.path || ''
    fullSize = false
    currentTime = 0
    duration = 0
    mediaPaused = true
  }
  function navigate(offset) {
    if (
      position >= 0 &&
      position + offset >= 0 &&
      position + offset < siblings.length
    )
      onNavigate(siblings[position + offset])
  }

  function activeMedia() {
    return isVideo ? videoEl : isAudio ? audioEl : null
  }

  function handleSeek(e) {
    const val = parseFloat(e.target.value)
    const el = activeMedia()
    if (el && Number.isFinite(val)) {
      el.currentTime = val
      currentTime = val
    }
  }

  function handleVolumeChange(e) {
    const val = parseFloat(e.target.value)
    volume = val
    const el = activeMedia()
    if (el) {
      el.volume = val
      el.muted = val === 0
      isMuted = val === 0
    }
  }

  function toggleMute() {
    const el = activeMedia()
    if (!el) return
    isMuted = !isMuted
    el.muted = isMuted
    if (!isMuted && el.volume === 0) {
      volume = 1
      el.volume = 1
    }
  }

  function togglePlay() {
    const el = activeMedia()
    if (!el) return
    if (el.paused) el.play?.().catch(() => {})
    else el.pause?.()
  }

  function skipBy(seconds) {
    const el = activeMedia()
    if (!el) return
    const max = Number.isFinite(el.duration) ? el.duration : Infinity
    el.currentTime = Math.max(0, Math.min(max, (el.currentTime || 0) + seconds))
  }

  function setSpeed(rate) {
    playbackRate = rate
    const el = activeMedia()
    if (el) el.playbackRate = rate
  }

  function togglePiP() {
    if (!videoEl) return
    if (document.pictureInPictureElement) {
      document.exitPictureInPicture?.().catch(() => {})
    } else {
      videoEl.requestPictureInPicture?.().catch(() => {})
    }
  }

  function captureSnapshot() {
    if (!videoEl || !videoEl.videoWidth) return
    const canvas = document.createElement('canvas')
    canvas.width = videoEl.videoWidth
    canvas.height = videoEl.videoHeight
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.drawImage(videoEl, 0, 0, canvas.width, canvas.height)
    const link = document.createElement('a')
    link.href = canvas.toDataURL('image/png')
    link.download = `${file?.name || 'video'}-snapshot.png`
    document.body.append(link)
    link.click()
    link.remove()
  }

  function handleMediaEnded() {
    mediaPaused = true
    if (loopMedia) return
    if (autoNext && position >= 0 && position + 1 < siblings.length) {
      navigate(1)
    }
  }

  function formatMediaTime(sec) {
    if (!Number.isFinite(sec) || sec < 0) return '0:00'
    const total = Math.floor(sec)
    const m = Math.floor(total / 60)
    const s = total % 60
    return `${m}:${s < 10 ? '0' : ''}${s}`
  }

  function drawAudioVisualizer() {
    if (!visualizerCanvas || !isAudio) return
    const ctx = visualizerCanvas.getContext('2d')
    if (!ctx) return
    const w = visualizerCanvas.width
    const h = visualizerCanvas.height
    ctx.clearRect(0, 0, w, h)
    const bars = 36
    const gap = 4
    const barW = (w - gap * (bars - 1)) / bars
    const t = performance.now() / 180
    for (let i = 0; i < bars; i++) {
      const wave = mediaPaused
        ? 0.14
        : 0.2 +
          0.75 * Math.abs(Math.sin(t + i * 0.35) * Math.cos(t * 0.7 - i * 0.18))
      const barH = Math.max(6, Math.round(wave * (h - 8)))
      const x = i * (barW + gap)
      const y = h - barH
      ctx.fillStyle = mediaPaused ? 'rgba(148,163,184,0.35)' : '#5ca7ff'
      ctx.beginPath()
      ctx.roundRect?.(x, y, barW, barH, 3) ?? ctx.rect(x, y, barW, barH)
      ctx.fill()
    }
    if (!destroyed && isAudio) {
      animFrame = requestAnimationFrame(drawAudioVisualizer)
    }
  }

  $: if (
    isAudio &&
    visualizerCanvas &&
    typeof requestAnimationFrame === 'function'
  ) {
    if (animFrame) cancelAnimationFrame(animFrame)
    animFrame = requestAnimationFrame(drawAudioVisualizer)
  }

  let previewRequest = 0
  let previewController = null
  let destroyed = false
  let loading = false
  let loadError = false
  $: mime = file?.mime_type || ''
  $: extension = file?.name?.includes('.')
    ? file.name.split('.').pop()?.toLowerCase() || ''
    : ''
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
  $: previewPath = file?.path ?? null
  $: token = encodeURIComponent($accessToken)
  $: sourceSuffix = getSourceParam($activeSourceId, $activeAgentId)
  $: previewUrl = file
    ? `/api/files/preview?path=${encodeURIComponent(file.path)}&access_token=${token}${sourceSuffix}`
    : ''
  $: downloadUrl = file
    ? `/api/files/download?path=${encodeURIComponent(file.path)}&access_token=${token}${sourceSuffix}`
    : ''
  $: loadPreview(previewPath, isText)

  async function loadPreview(path, textFile) {
    previewController?.abort()
    const request = ++previewRequest
    previewController = null
    loading = path !== null && textFile
    loadError = false
    textContent = ''
    if (path === null || !textFile) return

    const controller = new AbortController()
    previewController = controller
    try {
      const res = await apiFetch(
        `/api/files/download?path=${encodeURIComponent(path)}`,
        { signal: controller.signal }
      )
      if (!res.ok) throw new Error('Preview failed')
      const text = await res.text()
      if (destroyed || request !== previewRequest || controller.signal.aborted)
        return
      textContent = text
      loading = false
    } catch {
      if (
        !destroyed &&
        request === previewRequest &&
        !controller.signal.aborted
      ) {
        loading = false
        loadError = true
      }
    } finally {
      if (request === previewRequest) previewController = null
    }
  }

  function mediaError(event) {
    if (!destroyed && event.currentTarget.getAttribute('src') === previewUrl)
      loadError = true
  }

  onDestroy(() => {
    destroyed = true
    previewRequest++
    previewController?.abort()
    if (animFrame && typeof cancelAnimationFrame === 'function')
      cancelAnimationFrame(animFrame)
  })

  function handleKey(e) {
    if (
      !file ||
      e.defaultPrevented ||
      e.altKey ||
      e.ctrlKey ||
      e.metaKey ||
      e.shiftKey ||
      e.target?.closest?.('input,textarea,select,[contenteditable]')
    )
      return
    if (e.key === 'ArrowLeft') {
      e.preventDefault()
      navigate(-1)
    }
    if (e.key === 'ArrowRight') {
      e.preventDefault()
      navigate(1)
    }
    if ((isVideo || isAudio) && e.key === ' ') {
      e.preventDefault()
      togglePlay()
    }
  }
</script>

<svelte:window onkeydown={handleKey} />

{#if file}
  <Modal
    label={$i18n('Preview')}
    width={theaterMode ? '1160px' : '900px'}
    {onClose}
  >
    <div class="modal" class:theater={theaterMode}>
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
              disabled={position < 0 || position >= siblings.length - 1}
              onclick={() => navigate(1)}
              ><Icon name="chevron_r" size={16} /></button
            >{/if}
          {#if isImage}<button
              class="btn"
              onclick={() => (fullSize = !fullSize)}
              >{fullSize ? $i18n('Fit image') : $i18n('Original size')}</button
            >{/if}
          {#if isVideo}<button
              class="btn"
              onclick={() => (theaterMode = !theaterMode)}
              >{theaterMode ? 'Standard' : 'Cinema'}</button
            >{/if}
          {#if extension === 'md'}<button
              class="btn"
              onclick={() => (rawMarkdown = !rawMarkdown)}
              >{rawMarkdown
                ? $i18n('Formatted preview')
                : $i18n('Source text')}</button
            >{/if}
          <a href={downloadUrl} download={file.name} class="btn">
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
        {#key previewPath}
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
              src={previewUrl}
              alt={file.name}
              onerror={mediaError}
            />
          {:else if isVideo}
            <div class="media-studio">
              <div class="video-stage">
                <video
                  bind:this={videoEl}
                  controls
                  playsinline
                  width="960"
                  height="540"
                  preload="metadata"
                  loop={loopMedia}
                  src={previewUrl}
                  onplay={() => {
                    mediaPaused = false
                    if (videoEl) {
                      videoEl.playbackRate = playbackRate
                      videoEl.volume = volume
                      videoEl.muted = isMuted
                    }
                  }}
                  onpause={() => (mediaPaused = true)}
                  ontimeupdate={() => (currentTime = videoEl?.currentTime || 0)}
                  ondurationchange={() => (duration = videoEl?.duration || 0)}
                  onended={handleMediaEnded}
                  onerror={mediaError}><track kind="captions" /></video
                >
              </div>
              <div class="studio-timeline">
                <input
                  type="range"
                  class="timeline-slider"
                  min="0"
                  max={duration || 100}
                  step="0.1"
                  value={currentTime}
                  oninput={handleSeek}
                  aria-label={$i18n('Seek position')}
                />
              </div>
              <div class="studio-bar">
                <div class="studio-left">
                  <button class="btn" onclick={togglePlay}>
                    {mediaPaused ? '▶ Play' : '⏸ Pause'}
                  </button>
                  <button class="btn" onclick={() => skipBy(-10)}>-10s</button>
                  <button class="btn" onclick={() => skipBy(10)}>+10s</button>
                  <span class="time-pill">
                    {formatMediaTime(currentTime)} / {formatMediaTime(duration)}
                  </span>
                  <div class="volume-control">
                    <button
                      class="volume-btn"
                      onclick={toggleMute}
                      aria-label={isMuted ? 'Unmute' : 'Mute'}
                    >
                      <Icon
                        name={isMuted || volume === 0 ? 'volume_off' : 'volume'}
                        size={15}
                      />
                    </button>
                    <input
                      type="range"
                      class="volume-slider"
                      min="0"
                      max="1"
                      step="0.05"
                      value={isMuted ? 0 : volume}
                      oninput={handleVolumeChange}
                      aria-label="Volume"
                    />
                  </div>
                </div>
                <div class="studio-right">
                  <div
                    class="speed-group"
                    role="group"
                    aria-label="Playback speed"
                  >
                    {#each speeds as s}
                      <button
                        class="speed-pill"
                        class:active={playbackRate === s}
                        onclick={() => setSpeed(s)}>{s}x</button
                      >
                    {/each}
                  </div>
                  <button
                    class="btn"
                    class:primary={loopMedia}
                    onclick={() => (loopMedia = !loopMedia)}>🔁 Loop</button
                  >
                  <button
                    class="btn"
                    class:primary={autoNext}
                    onclick={() => (autoNext = !autoNext)}>⏭ Auto-Next</button
                  >
                  <button class="btn" onclick={togglePiP}>📺 PiP</button>
                  <button class="btn" onclick={captureSnapshot}
                    >📸 Snapshot</button
                  >
                </div>
              </div>
            </div>
          {:else if isAudio}
            <div class="audio-studio">
              <div class="audio-hero">
                <div class="audio-disc" class:spinning={!mediaPaused}>
                  <Icon name="audio" size={28} />
                </div>
                <div class="audio-meta">
                  <strong>{file.name}</strong>
                  <span
                    >{formatMediaTime(currentTime)} / {formatMediaTime(
                      duration
                    )}</span
                  >
                </div>
              </div>
              <canvas
                bind:this={visualizerCanvas}
                class="audio-visualizer"
                width="600"
                height="72"
              ></canvas>
              <audio
                bind:this={audioEl}
                controls
                preload="metadata"
                loop={loopMedia}
                src={previewUrl}
                onplay={() => {
                  mediaPaused = false
                  if (audioEl) {
                    audioEl.playbackRate = playbackRate
                    audioEl.volume = volume
                    audioEl.muted = isMuted
                  }
                }}
                onpause={() => (mediaPaused = true)}
                ontimeupdate={() => (currentTime = audioEl?.currentTime || 0)}
                ondurationchange={() => (duration = audioEl?.duration || 0)}
                onended={handleMediaEnded}
                onerror={mediaError}
              ></audio>
              <div class="studio-timeline">
                <input
                  type="range"
                  class="timeline-slider"
                  min="0"
                  max={duration || 100}
                  step="0.1"
                  value={currentTime}
                  oninput={handleSeek}
                  aria-label={$i18n('Seek position')}
                />
              </div>
              <div class="studio-bar">
                <div class="studio-left">
                  <button class="btn" onclick={togglePlay}>
                    {mediaPaused ? '▶ Play' : '⏸ Pause'}
                  </button>
                  <button class="btn" onclick={() => skipBy(-10)}>-10s</button>
                  <button class="btn" onclick={() => skipBy(10)}>+10s</button>
                  <div class="volume-control">
                    <button
                      class="volume-btn"
                      onclick={toggleMute}
                      aria-label={isMuted ? 'Unmute' : 'Mute'}
                    >
                      <Icon
                        name={isMuted || volume === 0 ? 'volume_off' : 'volume'}
                        size={15}
                      />
                    </button>
                    <input
                      type="range"
                      class="volume-slider"
                      min="0"
                      max="1"
                      step="0.05"
                      value={isMuted ? 0 : volume}
                      oninput={handleVolumeChange}
                      aria-label="Volume"
                    />
                  </div>
                </div>
                <div class="studio-right">
                  <div
                    class="speed-group"
                    role="group"
                    aria-label="Playback speed"
                  >
                    {#each speeds as s}
                      <button
                        class="speed-pill"
                        class:active={playbackRate === s}
                        onclick={() => setSpeed(s)}>{s}x</button
                      >
                    {/each}
                  </div>
                  <button
                    class="btn"
                    class:primary={loopMedia}
                    onclick={() => (loopMedia = !loopMedia)}>🔁 Loop</button
                  >
                  <button
                    class="btn"
                    class:primary={autoNext}
                    onclick={() => (autoNext = !autoNext)}>⏭ Auto-Next</button
                  >
                </div>
              </div>
              {#if audioSiblings.length > 1}
                <div class="audio-playlist">
                  <div class="playlist-head">
                    Playlist ({audioSiblings.length})
                  </div>
                  <div class="playlist-items">
                    {#each audioSiblings as track, idx}
                      <button
                        class="playlist-track"
                        class:current={track.path === file.path}
                        onclick={() => onNavigate(track)}
                      >
                        <span class="track-num">{idx + 1}.</span>
                        <span class="track-name">{track.name}</span>
                      </button>
                    {/each}
                  </div>
                </div>
              {/if}
            </div>
          {:else if isPdf}
            <iframe src={previewUrl} title={file.name}></iframe>
          {:else if isText}
            {#if extension === 'md' && !rawMarkdown}<article class="markdown">
                {@html markdown}
              </article>{:else}<pre>{textContent}</pre>{/if}
          {:else}
            <div class="unsupported">
              <Icon name="file" size={36} />
              <p>{$i18n('No preview available')}</p>
              <a href={downloadUrl} download={file.name} class="btn primary">
                <Icon name="download" size={13} />
                {$i18n('Download')}
              </a>
            </div>
          {/if}
        {/key}
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
  .modal.theater {
    max-width: 1160px;
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
  .media-studio,
  .audio-studio {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .audio-studio {
    max-width: 680px;
    background: var(--bg2);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 1.1rem;
  }
  .audio-hero {
    display: flex;
    align-items: center;
    gap: 0.9rem;
  }
  .audio-disc {
    width: 52px;
    height: 52px;
    border-radius: 50%;
    background: var(--surface2);
    border: 2px solid var(--accent);
    display: grid;
    place-items: center;
    color: var(--accent);
    flex-shrink: 0;
  }
  .audio-disc.spinning {
    animation: spin 6s linear infinite;
  }
  .audio-meta {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    min-width: 0;
  }
  .audio-meta strong {
    font-size: 0.95rem;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .audio-meta span {
    font-size: 0.78rem;
    color: var(--text2);
    font-variant-numeric: tabular-nums;
  }
  .audio-visualizer {
    width: 100%;
    height: 72px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 6px;
  }
  .studio-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.6rem;
    flex-wrap: wrap;
    background: var(--surface2);
    border: 1px solid var(--border);
    padding: 0.55rem 0.75rem;
    border-radius: 8px;
  }
  .studio-left,
  .studio-right {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex-wrap: wrap;
  }
  .time-pill {
    font-size: 0.75rem;
    color: var(--text2);
    font-variant-numeric: tabular-nums;
    padding: 0 0.35rem;
  }
  .speed-group {
    display: inline-flex;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 5px;
    overflow: hidden;
  }
  .speed-pill {
    background: transparent;
    border: none;
    color: var(--text2);
    font-size: 0.72rem;
    padding: 0.25rem 0.45rem;
    cursor: pointer;
  }
  .speed-pill.active {
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 600;
  }
  .audio-playlist {
    border-top: 1px solid var(--border);
    padding-top: 0.65rem;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .playlist-head {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text2);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .playlist-items {
    max-height: 150px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
  }
  .playlist-track {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 5px;
    padding: 0.35rem 0.55rem;
    color: var(--text);
    font-size: 0.8rem;
    cursor: pointer;
    text-align: left;
  }
  .playlist-track:hover {
    background: var(--row-hover);
  }
  .playlist-track.current {
    background: var(--row-active);
    border-color: var(--accent);
    color: var(--accent);
    font-weight: 600;
  }
  .track-num {
    color: var(--text2);
    font-variant-numeric: tabular-nums;
  }
  .track-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .video-stage {
    width: 100%;
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
  .studio-timeline {
    width: 100%;
    padding: 0.15rem 0;
  }
  .timeline-slider {
    width: 100%;
    height: 6px;
    -webkit-appearance: none;
    appearance: none;
    background: var(--border);
    border-radius: 9999px;
    outline: none;
    cursor: pointer;
    transition: height 0.15s ease;
  }
  .timeline-slider:hover {
    height: 8px;
  }
  .timeline-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.3);
  }
  .timeline-slider::-moz-range-thumb {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
    border: none;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.3);
  }
  .volume-control {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0 0.25rem;
  }
  .volume-btn {
    background: transparent;
    border: none;
    padding: 3px;
    cursor: pointer;
    color: var(--text2);
    display: flex;
    align-items: center;
    border-radius: 4px;
  }
  .volume-btn:hover {
    color: var(--text);
  }
  .volume-slider {
    width: 64px;
    height: 4px;
    -webkit-appearance: none;
    appearance: none;
    background: var(--border);
    border-radius: 9999px;
    outline: none;
    cursor: pointer;
  }
  .volume-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 11px;
    height: 11px;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
  }
  .volume-slider::-moz-range-thumb {
    width: 11px;
    height: 11px;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
    border: none;
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
  @media (prefers-reduced-motion: reduce) {
    .audio-disc.spinning,
    .spinner {
      animation: none !important;
    }
    .timeline-slider {
      transition: none;
    }
  }
</style>
