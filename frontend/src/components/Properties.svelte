<script>
  import { locale, translator as i18n } from '../stores/i18n.js'
  import { onMount, onDestroy } from 'svelte'
  import Dialog from './Dialog.svelte'
  import { apiFetch } from '../stores/auth.js'
  import { requireOK, formatBytes } from '../lib/files.js'
  export let path = ''
  export let onClose = () => {}
  let info,
    loading = true,
    issue = '',
    hashing = false,
    algorithm = 'sha256',
    checksum = '',
    controller = new AbortController()
  onMount(async () => {
    try {
      info = await (
        await requireOK(
          await apiFetch(`/api/files/info?${new URLSearchParams({ path })}`, {
            signal: controller.signal
          })
        )
      ).json()
    } catch (e) {
      if (!controller.signal.aborted) issue = e.message
    } finally {
      loading = false
    }
  })
  onDestroy(() => controller.abort())
  async function calculate() {
    hashing = true
    issue = ''
    checksum = ''
    try {
      const result = await (
        await requireOK(
          await apiFetch(
            `/api/files/info?${new URLSearchParams({ path, checksum: algorithm })}`,
            { signal: controller.signal }
          )
        )
      ).json()
      checksum = result.checksum
    } catch (e) {
      if (!controller.signal.aborted) issue = e.message
    } finally {
      hashing = false
    }
  }
</script>

<Dialog title={$i18n('File information')} {onClose}>
  {#if loading}<p class="dialog-hint">{$i18n('Loading…')}</p>{:else if info}
    <dl>
      <dt>{$i18n('Name')}</dt>
      <dd>{info.name}</dd>
      <dt>{$i18n('Path')}</dt>
      <dd>/{info.path}</dd>
      <dt>{$i18n('Type')}</dt>
      <dd>{info.is_dir ? $i18n('Folder') : info.mime_type}</dd>
      <dt>{$i18n('Size')}</dt>
      <dd>{formatBytes(info.size, $locale)}</dd>
      {#if info.width && info.height}<dt>{$i18n('Dimensions')}</dt>
        <dd>{info.width} × {info.height} px</dd>{/if}
      <dt>{$i18n('Last modified')}</dt>
      <dd>{new Date(info.mod_time).toLocaleString($locale)}</dd>
      <dt>{$i18n('Permissions')}</dt>
      <dd><code>{info.mode}</code></dd>
      {#if info.is_dir}<dt>{$i18n('Files')}</dt>
        <dd>{info.files}</dd>
        <dt>{$i18n('Folders')}</dt>
        <dd>{info.folders}</dd>{/if}
    </dl>
    {#if !info.is_dir}<label class="field"
        >{$i18n('Checksum')}<select
          bind:value={algorithm}
          onchange={() => (checksum = '')}
          ><option value="sha256">SHA-256</option><option value="sha512"
            >SHA-512</option
          ><option value="sha1">SHA-1</option><option value="md5">MD5</option
          ></select
        ></label
      ><button disabled={hashing} onclick={calculate}
        >{hashing ? $i18n('Calculating…') : $i18n('Calculate checksum')}</button
      >{#if checksum}<code class="checksum">{checksum}</code>{/if}{/if}
  {/if}
  {#if issue}<p class="dialog-error" role="alert">{issue}</p>{/if}
</Dialog>

<style>
  dl {
    display: grid;
    grid-template-columns: 110px 1fr;
    gap: 14px;
    font-size: 13px;
  }
  dt {
    color: var(--text2);
  }
  dd {
    overflow-wrap: anywhere;
    margin: 0;
  }
  .checksum {
    display: block;
    overflow-wrap: anywhere;
    padding: 14px;
    background: var(--surface2);
    border-radius: 7px;
    margin-top: 12px;
    font-size: 12px;
    user-select: all;
  }
</style>
