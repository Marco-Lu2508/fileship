<script>
  import Modal from './Modal.svelte'
  import { locale, translator as i18n } from '../stores/i18n.js'
  import { onMount } from 'svelte'
  import { apiFetch } from '../stores/auth.js'
  import { success, error } from '../stores/toast.js'
  import { loadFiles, currentPath } from '../stores/files.js'
  import { formatBytes } from '../lib/files.js'
  import { get } from 'svelte/store'
  import Icon from './Icon.svelte'

  export let onClose = () => {}

  let items = []
  let loading = true

  onMount(load)

  async function load() {
    loading = true
    const res = await apiFetch('/api/trash')
    if (res.ok) items = (await res.json()) ?? []
    loading = false
  }

  async function restore(trashName) {
    const res = await apiFetch(
      `/api/trash/${encodeURIComponent(trashName)}/restore`,
      { method: 'POST' }
    )
    if (res.ok) {
      success($i18n('Restored'))
      await load()
      await loadFiles(get(currentPath))
    } else {
      error($i18n('Restore failed'))
    }
  }

  async function deletePermanent(trashName) {
    if (!confirm($i18n('Permanently delete this item? This cannot be undone.')))
      return
    const res = await apiFetch(`/api/trash/${encodeURIComponent(trashName)}`, {
      method: 'DELETE'
    })
    if (res.ok) {
      success($i18n('Deleted permanently'))
      await load()
    } else error($i18n('Could not delete'))
  }

  async function emptyTrash() {
    if (
      !confirm(
        $i18n('Empty the entire trash? All items will be permanently deleted.')
      )
    )
      return
    const res = await apiFetch('/api/trash', { method: 'DELETE' })
    if (res.ok) {
      success($i18n('Trash emptied'))
      items = []
    } else error($i18n('Could not empty trash'))
  }

  function fmt(b) {
    return b ? formatBytes(b, $locale) : '—'
  }

  function daysLeft(expiresAt) {
    const d = Math.ceil((new Date(expiresAt) - Date.now()) / 86400000)
    return d > 0 ? d : 0
  }
</script>

<Modal label={$i18n('Trash')} width="560px" {onClose}>
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title">
        <span class="title-icon"><Icon name="trash" size={15} /></span>
        <div>
          <strong>{$i18n('Trash')}</strong>
          <small>{$i18n('Items are deleted permanently after 30 days')}</small>
        </div>
      </div>
      <div class="header-right">
        {#if items.length > 0}
          <button class="btn-danger-ghost" onclick={emptyTrash}>
            <Icon name="trash" size={13} />
            {$i18n('Empty all')}
          </button>
        {/if}
        <button class="close-btn" aria-label={$i18n('Close')} onclick={onClose}
          ><Icon name="x" size={14} /></button
        >
      </div>
    </div>

    <div class="modal-body">
      {#if loading}
        <div class="state-row">
          <span class="spinner"></span>
          {$i18n('Loading…')}
        </div>
      {:else if items.length === 0}
        <div class="empty-state">
          <div class="empty-icon"><Icon name="trash" size={28} /></div>
          <p>{$i18n('Trash is empty')}</p>
        </div>
      {:else}
        <div class="item-list">
          {#each items as item (item.id)}
            <div class="item-row">
              <span class="item-icon" class:is-dir={item.is_dir}>
                <Icon name={item.is_dir ? 'folder' : 'file'} size={16} />
              </span>
              <div class="item-meta">
                <span class="item-name">{item.name}</span>
                <span class="item-sub">
                  {fmt(item.size)}
                  {$i18n('· Deleted')}
                  {new Date(item.deleted_at).toLocaleDateString($locale)}
                  ·
                  <span class:expiring={daysLeft(item.expires_at) < 3}>
                    {$i18n('{count} days left', {
                      count: daysLeft(item.expires_at)
                    })}
                  </span>
                </span>
              </div>
              <div class="item-actions">
                <button
                  class="act-btn"
                  onclick={() => restore(item.trash_name)}
                  aria-label={$i18n('Restore')}
                >
                  <Icon name="refresh" size={14} />
                </button>
                <button
                  class="act-btn danger"
                  onclick={() => deletePermanent(item.trash_name)}
                  aria-label={$i18n('Delete permanently')}
                >
                  <Icon name="trash" size={14} />
                </button>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</Modal>

<style>
  .modal {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    width: 100%;
    max-width: 560px;
    max-height: calc(100dvh - 32px);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    box-shadow: var(--shadow-lg);
    animation: modal-in 0.2s cubic-bezier(0.2, 0.8, 0.3, 1) both;
  }
  @keyframes modal-in {
    from {
      opacity: 0;
      transform: scale(0.96) translateY(8px);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }
  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 1rem 1.1rem;
    border-bottom: 1px solid var(--border);
    gap: 0.75rem;
  }
  .modal-title {
    display: flex;
    align-items: center;
    gap: 0.65rem;
  }
  .title-icon {
    width: 32px;
    height: 32px;
    border-radius: var(--radius);
    background: var(--danger-bg);
    color: var(--danger);
    display: grid;
    place-items: center;
    flex-shrink: 0;
  }
  .modal-title strong {
    font-size: 0.95rem;
    display: block;
  }
  .modal-title small {
    font-size: 0.72rem;
    color: var(--text2);
  }
  .header-right {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }
  .close-btn {
    background: none;
    border: none;
    color: var(--text2);
    cursor: pointer;
    padding: 0.35rem;
    border-radius: var(--radius);
    display: flex;
  }
  .close-btn:hover {
    background: var(--row-hover);
    color: var(--text);
  }
  .btn-danger-ghost {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    background: none;
    border: 1px solid var(--danger);
    color: var(--danger);
    padding: 0.35rem 0.65rem;
    border-radius: var(--radius);
    font-size: 0.78rem;
    cursor: pointer;
    transition: background var(--transition);
  }
  .btn-danger-ghost:hover {
    background: var(--danger-bg);
  }
  .modal-body {
    overflow-y: auto;
    flex: 1;
  }
  .state-row {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    padding: 3rem;
    color: var(--text2);
    font-size: 0.85rem;
  }
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.75rem;
    padding: 4rem 2rem;
    color: var(--text3);
  }
  .empty-icon {
    width: 56px;
    height: 56px;
    border-radius: 50%;
    background: var(--surface2);
    border: 1px solid var(--border);
    display: grid;
    place-items: center;
  }
  .empty-state p {
    font-size: 0.9rem;
  }
  .item-list {
    display: flex;
    flex-direction: column;
  }
  .item-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1.1rem;
    border-bottom: 1px solid var(--border2);
    transition: background var(--transition);
  }
  .item-row:hover {
    background: var(--row-hover);
  }
  .item-icon {
    width: 32px;
    height: 32px;
    border-radius: var(--radius);
    background: var(--surface2);
    color: var(--text3);
    display: grid;
    place-items: center;
    flex-shrink: 0;
  }
  .item-icon.is-dir {
    color: var(--accent);
    background: var(--accent-soft);
  }
  .item-meta {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }
  .item-name {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .item-sub {
    font-size: 0.75rem;
    color: var(--text3);
  }
  .expiring {
    color: var(--warning);
    font-weight: 600;
  }
  .item-actions {
    display: flex;
    gap: 0.25rem;
    flex-shrink: 0;
  }
  .act-btn {
    background: none;
    border: none;
    color: var(--text2);
    cursor: pointer;
    padding: 0.35rem;
    border-radius: 5px;
    display: grid;
    place-items: center;
    transition:
      background var(--transition),
      color var(--transition);
  }
  .act-btn:hover {
    background: var(--surface2);
    color: var(--text);
  }
  .act-btn.danger:hover {
    background: var(--danger-bg);
    color: var(--danger);
  }
  .spinner {
    width: 14px;
    height: 14px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
