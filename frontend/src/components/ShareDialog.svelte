<script>
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'

  export let file = null
  export let onClose = () => {}

  let expiresIn = 24
  let shareUrl = ''
  let loading = false

  async function createShare() {
    loading = true
    const res = await apiFetch('/api/shares', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        path: file.path,
        is_dir: file.is_dir,
        expires_in_hours: expiresIn > 0 ? expiresIn : null
      })
    })
    loading = false
    if (!res.ok) { error('Could not create share link'); return }
    const { token } = await res.json()
    shareUrl = `${location.origin}/s/${token}`
  }

  function copy() {
    navigator.clipboard.writeText(shareUrl)
    success('Link copied!')
  }
</script>

{#if file}
  <div class="overlay" onclick={onClose} role="dialog" aria-modal="true">
    <div class="modal" onclick={(e) => e.stopPropagation()}>
      <div class="modal-header">
        <span>🔗 Share "{file.name}"</span>
        <button class="close" onclick={onClose}>✕</button>
      </div>
      <div class="modal-body">
        {#if !shareUrl}
          <label>
            Expires in (hours, 0 = never)
            <input type="number" min="0" bind:value={expiresIn} />
          </label>
          <button onclick={createShare} disabled={loading}>
            {loading ? 'Creating…' : 'Generate Link'}
          </button>
        {:else}
          <div class="link-row">
            <input readonly value={shareUrl} onclick={(e) => e.target.select()} />
            <button onclick={copy}>📋 Copy</button>
          </div>
          <p class="hint">Anyone with this link can download the file{expiresIn > 0 ? ` for ${expiresIn}h` : ''}.</p>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.8); display: flex; align-items: center; justify-content: center; z-index: 600; }
  .modal { background: #1a1d27; border: 1px solid #2a2d3a; border-radius: 12px; width: 100%; max-width: 480px; overflow: hidden; }
  .modal-header { display: flex; align-items: center; justify-content: space-between; padding: 1rem 1.25rem; border-bottom: 1px solid #2a2d3a; color: #e2e8f0; font-size: 0.95rem; }
  .close { background: none; border: none; color: #718096; cursor: pointer; font-size: 1.1rem; }
  .modal-body { padding: 1.5rem; display: flex; flex-direction: column; gap: 1rem; }
  label { display: flex; flex-direction: column; gap: 0.4rem; font-size: 0.85rem; color: #a0aec0; }
  input[type="number"], input[readonly] { background: #0f1117; border: 1px solid #2a2d3a; color: #fff; padding: 0.5rem 0.75rem; border-radius: 6px; font-size: 0.9rem; width: 100%; }
  button { background: #5865f2; border: none; color: #fff; padding: 0.6rem 1rem; border-radius: 6px; cursor: pointer; font-size: 0.9rem; }
  button:hover:not(:disabled) { background: #4752c4; }
  button:disabled { opacity: 0.6; }
  .link-row { display: flex; gap: 0.5rem; }
  .link-row input { flex: 1; }
  .link-row button { flex-shrink: 0; }
  .hint { font-size: 0.8rem; color: #718096; }
</style>
