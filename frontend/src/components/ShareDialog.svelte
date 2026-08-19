<script>
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import Icon from './Icon.svelte'

  export let file = null
  export let onClose = () => {}

  let expiresIn     = 24
  let password      = ''
  let downloadLimit = 0
  let allowUpload   = false
  let allowEdit     = false
  let shareUrl      = ''
  let loading       = false
  let step          = 'form'   // 'form' | 'done'

  $: if (!file) reset()

  function reset() {
    expiresIn = 24
    password = ''
    downloadLimit = 0
    allowUpload = false
    allowEdit = false
    shareUrl = ''
    loading = false
    step = 'form'
  }

  async function createShare() {
    loading = true
    const body = {
      path:           file.path,
      is_dir:         file.is_dir,
      expires_in_hours: expiresIn > 0 ? expiresIn : null,
      download_limit: downloadLimit > 0 ? downloadLimit : 0,
      allow_upload:   allowUpload,
      allow_edit:     allowEdit,
    }
    if (password) body.password = password

    const res = await apiFetch('/api/shares', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    })
    loading = false
    if (!res.ok) { error('Could not create share link'); return }
    const { token } = await res.json()
    shareUrl = `${location.origin}/s/${token}`
    step = 'done'
  }

  function copy() {
    navigator.clipboard.writeText(shareUrl)
    success('Link copied to clipboard')
  }

  function handleKey(e) { if (e.key === 'Escape') onClose() }
</script>

<svelte:window onkeydown={handleKey} />

{#if file}
  <div class="overlay" role="dialog" aria-modal="true" tabindex="-1"
    onclick={(e) => { if (e.target === e.currentTarget) onClose() }}
    onkeydown={handleKey}>
    <div class="modal">
      <div class="modal-header">
        <div class="modal-title">
          <span class="modal-icon"><Icon name="share" size={15} /></span>
          <div>
            <span class="title-text">Share</span>
            <span class="title-file">"{file.name}"</span>
          </div>
        </div>
        <button class="close-btn" onclick={onClose} title="Close">
          <Icon name="x" size={14} />
        </button>
      </div>

      <div class="modal-body">
        {#if step === 'form'}
          <!-- Expiry -->
          <div class="field-group">
            <label class="field-label">
              <Icon name="refresh" size={12} /> Expiry
            </label>
            <div class="radio-row">
              {#each [1, 24, 72, 168] as h}
                <button class="radio-btn" class:active={expiresIn === h} onclick={() => expiresIn = h}>
                  {h < 24 ? h + 'h' : (h / 24) + 'd'}
                </button>
              {/each}
              <button class="radio-btn" class:active={expiresIn === 0} onclick={() => expiresIn = 0}>
                Never
              </button>
            </div>
          </div>

          <!-- Password -->
          <div class="field-group">
            <label class="field-label" for="share-pw">
              <Icon name="lock" size={12} /> Password protection (optional)
            </label>
            <input id="share-pw" type="password" bind:value={password}
              placeholder="Leave empty for no password" />
          </div>

          <!-- Download limit -->
          <div class="field-group">
            <label class="field-label" for="share-dl-limit">
              <Icon name="download" size={12} /> Download limit (0 = unlimited)
            </label>
            <input id="share-dl-limit" type="number" min="0" bind:value={downloadLimit} />
          </div>

          <!-- Permissions (dir only) -->
          {#if file.is_dir}
            <div class="field-group">
              <label class="field-label">
                <Icon name="lock" size={12} /> Permissions
              </label>
              <div class="toggle-row">
                <label class="toggle-item">
                  <input type="checkbox" bind:checked={allowUpload} />
                  <span>Allow upload</span>
                </label>
                <label class="toggle-item">
                  <input type="checkbox" bind:checked={allowEdit} />
                  <span>Allow edit</span>
                </label>
              </div>
            </div>
          {/if}

          <button class="generate-btn" onclick={createShare} disabled={loading}>
            {#if loading}
              <span class="btn-spin"></span> Generating…
            {:else}
              <Icon name="link" size={14} /> Generate link
            {/if}
          </button>

        {:else}
          <!-- Done state -->
          <div class="done-icon">
            <Icon name="check" size={20} />
          </div>
          <p class="done-title">Link ready</p>

          <div class="link-box">
            <input readonly value={shareUrl} onclick={(e) => e.target.select()} aria-label="Share URL" />
            <button class="copy-btn" onclick={copy} title="Copy link">
              <Icon name="clipboard" size={14} />
            </button>
          </div>

          <div class="share-meta">
            {#if expiresIn > 0}
              <span><Icon name="refresh" size={11} /> Expires in {expiresIn < 24 ? expiresIn + 'h' : (expiresIn/24) + 'd'}</span>
            {:else}
              <span><Icon name="refresh" size={11} /> No expiry</span>
            {/if}
            {#if password}
              <span><Icon name="lock" size={11} /> Password protected</span>
            {/if}
            {#if downloadLimit > 0}
              <span><Icon name="download" size={11} /> {downloadLimit} DL limit</span>
            {/if}
          </div>

          <button class="btn-secondary" onclick={reset}>
            Create another link
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed; inset: 0;
    background: rgba(0,0,0,0.65);
    display: flex; align-items: center; justify-content: center;
    z-index: 700; padding: 1rem;
    backdrop-filter: blur(2px);
  }

  .modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    width: 100%; max-width: 460px;
    overflow: hidden;
    box-shadow: var(--shadow-lg);
    animation: modal-in 0.2s cubic-bezier(.2,.8,.3,1) both;
  }

  @keyframes modal-in {
    from { opacity: 0; transform: scale(0.95) translateY(10px); }
    to   { opacity: 1; transform: none; }
  }

  .modal-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 1rem 1.1rem;
    border-bottom: 1px solid var(--border);
  }

  .modal-title {
    display: flex; align-items: center; gap: 0.65rem;
  }

  .modal-icon {
    width: 32px; height: 32px;
    display: grid; place-items: center;
    background: var(--accent-soft); color: var(--accent);
    border-radius: 8px; flex-shrink: 0;
  }

  .title-text {
    font-weight: 700; font-size: 0.9rem; color: var(--text);
    display: block; line-height: 1.2;
  }

  .title-file {
    font-size: 0.75rem; color: var(--text2);
    overflow: hidden; text-overflow: ellipsis;
    white-space: nowrap; max-width: 250px; display: block;
  }

  .close-btn {
    background: none; border: none; color: var(--text2);
    cursor: pointer; padding: 0.35rem; border-radius: var(--radius);
    display: flex; transition: background var(--transition);
  }
  .close-btn:hover { background: var(--row-hover); color: var(--text); }

  .modal-body {
    padding: 1.25rem;
    display: flex; flex-direction: column; gap: 1rem;
  }

  .field-group { display: flex; flex-direction: column; gap: 0.4rem; }

  .field-label {
    font-size: 0.73rem; font-weight: 700;
    color: var(--text3); text-transform: uppercase;
    letter-spacing: 0.07em;
    display: flex; align-items: center; gap: 0.3rem;
  }

  .radio-row { display: flex; gap: 0.35rem; flex-wrap: wrap; }

  .radio-btn {
    padding: 0.3rem 0.7rem;
    background: var(--surface2); border: 1px solid var(--border);
    color: var(--text2); font-size: 0.8rem; cursor: pointer;
    border-radius: 20px; transition: all var(--transition);
  }

  .radio-btn.active {
    background: var(--accent-soft); border-color: var(--accent);
    color: var(--accent); font-weight: 600;
  }

  input[type="password"],
  input[type="number"],
  input[readonly] {
    background: var(--input-bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.55rem 0.75rem;
    border-radius: var(--radius); font-size: 0.875rem; width: 100%;
    transition: border-color var(--transition), box-shadow var(--transition);
  }

  input:focus {
    outline: none; border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }

  .toggle-row { display: flex; gap: 1rem; }

  .toggle-item {
    display: flex; align-items: center; gap: 0.4rem;
    font-size: 0.85rem; color: var(--text2); cursor: pointer;
  }

  .toggle-item input { width: auto; accent-color: var(--accent); }

  .generate-btn {
    display: flex; align-items: center; justify-content: center; gap: 0.5rem;
    width: 100%; padding: 0.7rem;
    background: var(--accent); border: none; color: #fff;
    border-radius: var(--radius); font-size: 0.9rem; font-weight: 600;
    cursor: pointer; transition: background var(--transition), box-shadow var(--transition);
    box-shadow: 0 2px 8px rgba(79,158,255,0.3);
  }

  .generate-btn:hover:not(:disabled) { background: var(--accent-h); box-shadow: 0 4px 14px rgba(79,158,255,0.4); }
  .generate-btn:disabled { opacity: 0.6; cursor: not-allowed; }

  .btn-spin {
    width: 14px; height: 14px;
    border: 2px solid rgba(255,255,255,0.4);
    border-top-color: #fff; border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* Done state */
  .done-icon {
    width: 48px; height: 48px; border-radius: 50%;
    background: var(--success-bg); color: var(--success);
    display: grid; place-items: center; margin: 0 auto;
    border: 2px solid var(--success);
    animation: pop-in 0.3s cubic-bezier(.2,.8,.3,1) both;
  }

  @keyframes pop-in {
    from { transform: scale(0.5); opacity: 0; }
    to   { transform: none; opacity: 1; }
  }

  .done-title {
    text-align: center; font-size: 1rem; font-weight: 700; color: var(--text);
  }

  .link-box { display: flex; gap: 0.4rem; }
  .link-box input { flex: 1; }

  .copy-btn {
    display: flex; align-items: center; justify-content: center;
    padding: 0.55rem 0.75rem;
    background: var(--surface2); border: 1px solid var(--border);
    color: var(--text2); border-radius: var(--radius);
    cursor: pointer; transition: background var(--transition);
    flex-shrink: 0;
  }
  .copy-btn:hover { background: var(--row-hover); color: var(--accent); }

  .share-meta {
    display: flex; flex-wrap: wrap; gap: 0.5rem;
  }

  .share-meta span {
    display: flex; align-items: center; gap: 0.3rem;
    font-size: 0.75rem; color: var(--text3);
    background: var(--surface2); border: 1px solid var(--border);
    padding: 0.2rem 0.55rem; border-radius: 20px;
  }

  .btn-secondary {
    background: none; border: 1px solid var(--border);
    color: var(--text2); padding: 0.5rem;
    border-radius: var(--radius); cursor: pointer; font-size: 0.82rem;
    width: 100%; transition: background var(--transition);
  }
  .btn-secondary:hover { background: var(--row-hover); color: var(--text); }
</style>
