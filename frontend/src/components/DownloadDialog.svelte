<script>
  import { translator as i18n } from '../stores/i18n.js'
  import Dialog from './Dialog.svelte'
  import { apiFetch } from '../stores/auth.js'
  import { saveDownload } from '../lib/files.js'
  export let paths = [],
    onClose = () => {}
  let format = 'zip',
    busy = false,
    issue = ''
  async function download() {
    busy = true
    issue = ''
    try {
      await saveDownload(
        await apiFetch('/api/files/archive', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ paths, format })
        }),
        'fileship-download.' + (format === 'targz' ? 'tar.gz' : format)
      )
      busy = false
      onClose()
    } catch (e) {
      issue = e.message
      busy = false
    }
  }
</script>

<Dialog title={$i18n('Download archive')} {busy} {onClose}>
  <p class="dialog-hint">
    {$i18n('{count} selected', { count: paths.length })}
  </p>
  <label class="field"
    >{$i18n('Archive format')}<select bind:value={format} disabled={busy}
      ><option value="zip">ZIP</option><option value="tar">TAR</option><option
        value="targz">TAR.GZ</option
      ></select
    ></label
  >
  {#if issue}<p class="dialog-error" role="alert">{issue}</p>{/if}
  <div class="dialog-footer">
    <button disabled={busy} onclick={onClose}>{$i18n('Cancel')}</button><button
      class="dialog-primary"
      disabled={busy}
      onclick={download}
      >{busy ? $i18n('Preparing…') : $i18n('Download')}</button
    >
  </div>
</Dialog>
