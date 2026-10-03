<script>
  import { locale, translator as i18n } from '../stores/i18n.js'
  import { onMount } from 'svelte'
  import Dialog from './Dialog.svelte'
  import ConfirmDialog from './ConfirmDialog.svelte'
  import { apiFetch } from '../stores/auth.js'
  import { requireOK } from '../lib/files.js'
  import { success, error } from '../stores/toast.js'
  export let onClose = () => {}
  let links = [],
    loading = true,
    issue = '',
    revoking = null,
    busy = false
  onMount(load)
  async function load() {
    loading = true
    issue = ''
    try {
      links = await (await requireOK(await apiFetch('/api/shares'))).json()
    } catch (e) {
      issue = e.message
    } finally {
      loading = false
    }
  }
  function linkUrl(token) {
    return `${location.origin}/s/${encodeURIComponent(token)}`
  }
  async function copy(token) {
    try {
      await navigator.clipboard.writeText(linkUrl(token))
      success($i18n('Link copied'))
    } catch {
      error($i18n('Could not copy link'))
    }
  }
  async function revoke() {
    busy = true
    try {
      await requireOK(
        await apiFetch(`/api/shares/${encodeURIComponent(revoking.token)}`, {
          method: 'DELETE'
        })
      )
      revoking = null
      await load()
      success($i18n('Share revoked'))
    } catch (e) {
      error(e.message)
    } finally {
      busy = false
    }
  }
</script>

<Dialog title={$i18n('Shared links')} {onClose}>
  {#if loading}<p class="dialog-hint">{$i18n('Loading…')}</p>{:else if issue}<p
      class="dialog-error"
    >
      {issue}
    </p>
    <button onclick={load}>{$i18n('Retry')}</button>
  {:else}{#each links as link}<article>
        <strong>{link.path}</strong>
        <p class="dialog-hint">
          {link.expires_at
            ? new Date(link.expires_at).toLocaleString($locale)
            : $i18n('Never expires')} · {link.has_password
            ? $i18n('Password protected')
            : $i18n('No password')} · {link.download_count} / {link.download_limit ||
            '∞'}
        </p>
        {#if link.expires_at && new Date(link.expires_at) <= new Date()}<span
            class="expired">{$i18n('Expired')}</span
          >{:else if link.download_limit > 0 && link.download_count >= link.download_limit}<span
            class="expired">{$i18n('Download limit reached')}</span
          >{/if}
        <div class="link-actions">
          <input
            readonly
            value={linkUrl(link.token)}
            aria-label={$i18n('Share link')}
          /><button onclick={() => copy(link.token)}
            >{$i18n('Copy link')}</button
          ><button onclick={() => (revoking = link)}>{$i18n('Revoke')}</button>
        </div>
      </article>{:else}<p class="dialog-hint">
        {$i18n('No shared links yet.')}
      </p>{/each}{/if}
</Dialog>
{#if revoking}<ConfirmDialog
    title={$i18n('Revoke this shared link?')}
    description={$i18n('People with this link will no longer have access.')}
    confirmLabel={$i18n('Revoke')}
    busyLabel={$i18n('Working…')}
    {busy}
    onConfirm={revoke}
    onClose={() => {
      if (!busy) revoking = null
    }}
  />{/if}

<style>
  article {
    padding: 14px 0;
    border-bottom: 1px solid var(--border);
    font-size: 13px;
  }
  strong {
    overflow-wrap: anywhere;
  }
  .link-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .link-actions input {
    flex: 1;
    min-width: 150px;
  }
  .expired {
    color: var(--danger);
    font-size: 12px;
    display: block;
    margin-bottom: 10px;
  }
</style>
