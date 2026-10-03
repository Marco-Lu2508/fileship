<script>
  import { translator as i18n } from '../stores/i18n.js'
  import Dialog from './Dialog.svelte'
  export let onClose = () => {}
  $: shortcuts = [
    ['Ctrl / ⌘ + K', $i18n('Search files')],
    ['Ctrl / ⌘ + F', $i18n('Search files')],
    ['Ctrl / ⌘ + click', $i18n('Toggle selection')],
    ['Shift + click', $i18n('Select a range')],
    ['Ctrl / ⌘ + A', $i18n('Select all visible files')],
    ['Ctrl / ⌘ + C', $i18n('Copy selected items')],
    ['Ctrl / ⌘ + X', $i18n('Cut selected items')],
    ['Ctrl / ⌘ + V', $i18n('Choose where to paste')],
    ['F2', $i18n('Rename selected item')],
    ['Delete', $i18n('Move selected items to trash')],
    ['Escape', $i18n('Close dialog or clear selection')],
    ['Alt + ↑', $i18n('Open parent folder')],
    ['R', $i18n('Refresh folder')],
    ['F1', $i18n('Keyboard shortcuts')],
    ['Ctrl / ⌘ + S', $i18n('Save in editor')],
    ['← / →', $i18n('Previous / next preview')]
  ]
</script>

<Dialog title={$i18n('Keyboard shortcuts')} {onClose}
  ><dl>
    {#each shortcuts as [key, description]}<dt><kbd>{key}</kbd></dt>
      <dd>{description}</dd>{/each}
  </dl></Dialog
>

<style>
  dl {
    display: grid;
    grid-template-columns: 155px 1fr;
    gap: 15px;
    font-size: 13px;
  }
  dd {
    margin: 0;
    color: var(--text2);
  }
  kbd {
    font-size: 11px;
    padding: 4px 6px;
    background: var(--surface2);
    border: 1px solid var(--border);
    border-radius: 4px;
  }
  @media (max-width: 400px) {
    dl {
      grid-template-columns: 1fr;
      gap: 9px;
    }
    dd {
      margin-bottom: 8px;
    }
  }
</style>
