<script>
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'

  export let file = null
  export let onClose = () => {}

  let content = ''
  let original = ''
  let loading = true
  let saving = false

  $: if (file) loadContent()

  async function loadContent() {
    loading = true
    const res = await apiFetch(`/api/files/text?path=${encodeURIComponent(file.path)}`)
    if (!res.ok) {
      error('Cannot open file')
      onClose()
      return
    }
    const data = await res.json()
    content = data.content
    original = data.content
    loading = false
  }

  async function save() {
    saving = true
    const res = await apiFetch('/api/files/text', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: file.path, content })
    })
    saving = false
    if (res.ok) {
      original = content
      success('Saved')
    } else {
      error('Save failed')
    }
  }

  function handleClose() {
    if (content !== original && !confirm('You have unsaved changes. Leave anyway?')) return
    onClose()
  }

  function handleKey(e) {
    if (e.key === 'Escape') handleClose()
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      e.preventDefault()
      save()
    }
  }

  $: isDirty = content !== original
  $: lineCount = content.split('\n').length
</script>

<svelte:window onkeydown={handleKey} />

{#if file}
  <div class="overlay" role="dialog" aria-modal="true">
    <div class="editor-modal">
      <div class="editor-header">
        <div class="file-info">
          <span class="filename">✏️ {file.name}</span>
          {#if isDirty}<span class="dirty">●</span>{/if}
        </div>
        <div class="editor-actions">
          <span class="meta">{lineCount} lines</span>
          <button onclick={save} disabled={saving || !isDirty} class="save-btn">
            {saving ? 'Saving…' : '💾 Save (Ctrl+S)'}
          </button>
          <button class="close" onclick={handleClose}>✕</button>
        </div>
      </div>
      <div class="editor-body">
        {#if loading}
          <div class="editor-loading">Loading…</div>
        {:else}
          <div class="line-numbers" aria-hidden="true">
            {#each Array(lineCount) as _, i}
              <span>{i + 1}</span>
            {/each}
          </div>
          <textarea
            bind:value={content}
            spellcheck="false"
            autocomplete="off"
            autocorrect="off"
            autocapitalize="off"
          ></textarea>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed; inset: 0; background: rgba(0,0,0,0.9);
    display: flex; align-items: center; justify-content: center;
    z-index: 700; padding: 1rem;
  }
  .editor-modal {
    background: #0d1117; border: 1px solid var(--border);
    border-radius: 12px; width: 100%; max-width: 1000px;
    height: 85vh; display: flex; flex-direction: column; overflow: hidden;
  }
  .editor-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 0.75rem 1rem; border-bottom: 1px solid var(--border);
    background: var(--surface); gap: 1rem; flex-wrap: wrap;
  }
  .file-info { display: flex; align-items: center; gap: 0.5rem; }
  .filename { font-size: 0.95rem; color: var(--text); font-family: monospace; }
  .dirty { color: #f59e0b; font-size: 1.2rem; line-height: 1; }
  .editor-actions { display: flex; align-items: center; gap: 0.75rem; }
  .meta { font-size: 0.8rem; color: var(--muted); }
  .save-btn {
    background: var(--accent); border: none; color: #fff;
    padding: 0.4rem 0.9rem; border-radius: 6px; cursor: pointer; font-size: 0.85rem;
  }
  .save-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .save-btn:hover:not(:disabled) { background: var(--accent-h); }
  .close { background: none; border: none; color: var(--muted); cursor: pointer; font-size: 1.1rem; padding: 0.25rem 0.5rem; border-radius: 4px; }
  .close:hover { background: var(--border); color: var(--text); }
  .editor-body {
    flex: 1; display: flex; overflow: hidden;
    font-family: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace;
    font-size: 0.875rem; line-height: 1.6;
  }
  .line-numbers {
    display: flex; flex-direction: column; align-items: flex-end;
    padding: 0.75rem 0.75rem 0.75rem 1rem;
    background: #0d1117; color: #4a5568;
    border-right: 1px solid var(--border);
    user-select: none; overflow: hidden; min-width: 3rem;
    font-size: 0.875rem; line-height: 1.6;
  }
  .line-numbers span { display: block; }
  textarea {
    flex: 1; background: #0d1117; color: #e2e8f0;
    border: none; outline: none; resize: none;
    padding: 0.75rem 1rem; font-family: inherit;
    font-size: inherit; line-height: inherit;
    tab-size: 2;
  }
  .editor-loading { padding: 2rem; color: var(--muted); margin: auto; }
</style>
