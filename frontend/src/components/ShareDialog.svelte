<script>
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import Icon from './Icon.svelte'

  export let file = null
  export let onClose = () => {}

  let expiresIn = 24
  let shareUrl  = ''
  let loading   = false

  async function createShare() {
    loading = true
    const res = await apiFetch('/api/shares', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: file.path, is_dir: file.is_dir, expires_in_hours: expiresIn > 0 ? expiresIn : null })
    })
    loading = false
    if (!res.ok) { error('Could not create share link'); return }
    const { token } = await res.json()
    shareUrl = `${location.origin}/s/${token}`
  }

  function copy() { navigator.clipboard.writeText(shareUrl); success('Link copied') }
  function handleKey(e) { if (e.key === 'Escape') onClose() }
</script>

<svelte:window onkeydown={handleKey} />

{#if file}
  <div class="overlay" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => { if (e.target === e.currentTarget) onClose() }} onkeydown={handleKey}>
    <div class="modal">
      <div class="modal-header">
        <span>Share "{file.name}"</span>
        <button class="close-btn" onclick={onClose}><Icon name="x" size={14} /></button>
      </div>
      <div class="modal-body">
        {#if !shareUrl}
          <label class="field">
            <span>Expires in hours (0 = never)</span>
            <input type="number" min="0" bind:value={expiresIn} />
          </label>
          <button class="btn primary" onclick={createShare} disabled={loading}>
            <Icon name="link" size={13} />
            {loading ? 'Creating...' : 'Generate link'}
          </button>
        {:else}
          <div class="link-row">
            <input readonly value={shareUrl} onclick={(e) => e.target.select()} />
            <button class="btn" onclick={copy}><Icon name="clipboard" size={13} /></button>
          </div>
          <p class="hint">Anyone with this link can download{expiresIn > 0 ? ` (expires in ${expiresIn}h)` : ''}.</p>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.7); display: flex; align-items: center; justify-content: center; z-index: 600; }
  .modal { background: var(--surface); border: 1px solid var(--border); border-radius: 6px; width: 100%; max-width: 440px; overflow: hidden; box-shadow: var(--shadow); }
  .modal-header { display: flex; align-items: center; justify-content: space-between; padding: 0.75rem 1rem; border-bottom: 1px solid var(--border); font-size: 0.875rem; color: var(--text); }
  .close-btn { background: none; border: none; color: var(--text2); cursor: pointer; padding: 0.3rem; border-radius: 3px; display: flex; }
  .close-btn:hover { background: var(--border); }
  .modal-body { padding: 1.25rem; display: flex; flex-direction: column; gap: 0.75rem; }
  .field { display: flex; flex-direction: column; gap: 0.3rem; font-size: 0.82rem; color: var(--text2); }
  input[type="number"], input[readonly] { background: var(--input-bg); border: 1px solid var(--border); color: var(--text); padding: 0.45rem 0.65rem; border-radius: 3px; font-size: 0.875rem; width: 100%; }
  input:focus { outline: none; border-color: var(--accent); }
  .btn { display: inline-flex; align-items: center; gap: 0.35rem; background: var(--bg2); border: 1px solid var(--border); color: var(--text); padding: 0.4rem 0.75rem; border-radius: 3px; cursor: pointer; font-size: 0.82rem; }
  .btn:hover:not(:disabled) { background: var(--row-hover); }
  .btn.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
  .btn.primary:hover:not(:disabled) { background: var(--accent-h); }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .link-row { display: flex; gap: 0.4rem; }
  .link-row input { flex: 1; }
  .hint { font-size: 0.78rem; color: var(--text2); }
</style>
