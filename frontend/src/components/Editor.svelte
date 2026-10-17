<script>
  import { onDestroy } from 'svelte'
  import Modal from './Modal.svelte'
  import { translator as i18n } from '../stores/i18n.js'
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import Icon from './Icon.svelte'

  export let file = null
  export let onClose = () => {}

  let content = ''
  let original = ''
  let loading = true
  let saving = false
  let loadId = 0
  let loadController = null
  let destroyed = false
  let lineNumbers

  $: currentPath = file?.path ?? null
  $: loadContent(currentPath)

  async function loadContent(path) {
    loadController?.abort()
    loadController = null
    const id = ++loadId
    loading = true
    content = original = ''
    if (path === null) {
      loading = false
      return
    }
    const controller = new AbortController()
    loadController = controller
    try {
      const res = await apiFetch(
        `/api/files/text?path=${encodeURIComponent(path)}`,
        { signal: controller.signal }
      )
      if (!res.ok) throw new Error('Cannot open file')
      const data = await res.json()
      if (destroyed || id !== loadId || controller.signal.aborted) return
      content = original = data.content
    } catch {
      if (destroyed || id !== loadId || controller.signal.aborted) return
      error($i18n('Cannot open file'))
      onClose()
    } finally {
      if (!destroyed && id === loadId) {
        loading = false
        loadController = null
      }
    }
  }

  async function save() {
    if (!file || loading || saving || content === original) return
    const path = file.path
    const id = loadId
    const savedContent = content
    saving = true
    try {
      const res = await apiFetch('/api/files/text', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path, content: savedContent })
      })
      if (!res.ok) throw new Error('Save failed')
      if (!destroyed && id === loadId && file?.path === path) {
        original = savedContent
        success($i18n('Saved'))
      }
    } catch {
      if (!destroyed && id === loadId) error($i18n('Save failed'))
    } finally {
      saving = false
    }
  }

  function handleClose() {
    if (saving) return
    if (
      content !== original &&
      !confirm($i18n('You have unsaved changes. Leave anyway?'))
    )
      return
    onClose()
  }

  function handleKey(e) {
    if (!file) return
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
      e.preventDefault()
      save()
    }
  }

  onDestroy(() => {
    destroyed = true
    loadId++
    loadController?.abort()
  })

  $: isDirty = content !== original
  $: lineCount = content.split('\n').length
</script>

<svelte:window
  onkeydown={handleKey}
  onbeforeunload={(event) => {
    if (file && (isDirty || saving)) {
      event.preventDefault()
      event.returnValue = ''
    }
  }}
/>

{#if file}
  <Modal
    label={$i18n('Edit')}
    width="1000px"
    onClose={handleClose}
    busy={saving}
  >
    <div class="editor-modal">
      <div class="editor-header">
        <div class="file-info">
          <span class="filename"
            ><Icon name="edit" size={14} /> {file.name}</span
          >
          {#if isDirty}<span class="dirty">●</span>{/if}
        </div>
        <div class="editor-actions">
          <span class="meta"
            >{$i18n('{count} lines', { count: lineCount })}</span
          >
          <button
            onclick={save}
            disabled={loading || saving || !isDirty}
            class="save-btn"
          >
            {saving ? $i18n('Saving…') : $i18n('Save (Ctrl+S)')}
          </button>
          <button
            class="close"
            disabled={saving}
            aria-label={$i18n('Close')}
            onclick={handleClose}><Icon name="x" size={19} /></button
          >
        </div>
      </div>
      <div class="editor-body">
        {#if loading}
          <div class="editor-loading">{$i18n('Loading…')}</div>
        {:else}
          <div class="line-numbers" bind:this={lineNumbers} aria-hidden="true">
            {#each Array(lineCount) as _, i}
              <span>{i + 1}</span>
            {/each}
          </div>
          <textarea
            aria-label={$i18n('Edit') + ': ' + file.name}
            bind:value={content}
            onscroll={(event) => {
              if (lineNumbers)
                lineNumbers.scrollTop = event.currentTarget.scrollTop
            }}
            spellcheck="false"
            autocomplete="off"
            autocorrect="off"
            autocapitalize="off"></textarea>
        {/if}
      </div>
    </div>
  </Modal>
{/if}

<style>
  .editor-modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 12px;
    width: 100%;
    max-width: 1000px;
    height: min(85dvh, 800px);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .editor-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border);
    background: var(--surface);
    gap: 1rem;
    flex-wrap: wrap;
  }
  .file-info {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .filename {
    overflow-wrap: anywhere;
    font-size: 0.95rem;
    color: var(--text);
    font-family: monospace;
  }
  .dirty {
    color: #f59e0b;
    font-size: 1.2rem;
    line-height: 1;
  }
  .editor-actions {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }
  .meta {
    font-size: 0.8rem;
    color: var(--muted);
  }
  .save-btn {
    background: var(--accent);
    border: none;
    color: var(--on-accent);
    padding: 0.4rem 0.9rem;
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.85rem;
  }
  .save-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .save-btn:hover:not(:disabled) {
    background: var(--accent-h);
  }
  .close {
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    font-size: 1.1rem;
    padding: 0.25rem 0.5rem;
    border-radius: 4px;
  }
  .close:hover {
    background: var(--border);
    color: var(--text);
  }
  .editor-body {
    flex: 1;
    display: flex;
    overflow: hidden;
    font-family: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace;
    font-size: 0.875rem;
    line-height: 1.6;
  }
  .line-numbers {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    padding: 0.75rem 0.75rem 0.75rem 1rem;
    background: var(--surface);
    color: var(--text3);
    border-right: 1px solid var(--border);
    user-select: none;
    overflow: hidden;
    min-width: 3rem;
    font-size: 0.875rem;
    line-height: 1.6;
  }
  .line-numbers span {
    display: block;
  }
  textarea {
    flex: 1;
    min-width: 0;
    background: var(--surface);
    color: var(--text);
    border: none;
    outline: none;
    resize: none;
    padding: 0.75rem 1rem;
    font-family: inherit;
    font-size: inherit;
    line-height: inherit;
    tab-size: 2;
  }
  .editor-loading {
    padding: 2rem;
    color: var(--muted);
    margin: auto;
  }
</style>
