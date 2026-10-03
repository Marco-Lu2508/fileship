<script>
  import { translator as i18n } from '../stores/i18n.js'
  import { onMount } from 'svelte'
  import Icon from './Icon.svelte'
  export let title
  export let busy = false
  export let onClose = () => {}
  let dialog
  onMount(() => {
    const opener = document.activeElement
    dialog.showModal()
    return () => {
      dialog.close()
      if (opener?.isConnected) opener.focus()
    }
  })
</script>

<dialog
  bind:this={dialog}
  aria-label={title}
  oncancel={(e) => {
    e.preventDefault()
    if (!busy) onClose()
  }}
>
  <header>
    <h2>{title}</h2>
    <button disabled={busy} aria-label={$i18n('Close')} onclick={onClose}
      ><Icon name="x" size={19} /></button
    >
  </header>
  <div class="dialog-body"><slot /></div>
</dialog>

<style>
  dialog {
    margin: auto;
    width: min(620px, calc(100% - 28px));
    max-height: calc(100dvh - 32px);
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: 12px;
    color: var(--text);
    background: var(--surface);
    box-shadow: var(--shadow-lg);
    padding: 0;
  }
  dialog::backdrop {
    background: #10201688;
    backdrop-filter: blur(3px);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 18px 22px;
    flex-shrink: 0;
    border-bottom: 1px solid var(--border);
  }
  h2 {
    font-size: 20px;
    font-weight: 600;
  }
  header button {
    border: 0;
    background: none;
    color: var(--text2);
    padding: 10px;
    cursor: pointer;
  }
  .dialog-body {
    padding: 20px 22px;
    overflow: auto;
    overscroll-behavior: contain;
  }
  .dialog-body :global(button),
  .dialog-body :global(input),
  .dialog-body :global(select) {
    font: inherit;
    font-size: 14px;
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text);
    background: var(--input-bg);
    padding: 9px 12px;
  }
  .dialog-body :global(button) {
    cursor: pointer;
    background: var(--surface2);
  }
  .dialog-body :global(button:disabled) {
    opacity: 0.5;
    cursor: default;
  }
  .dialog-body :global(.dialog-primary) {
    background: var(--accent);
    color: var(--on-accent);
    border-color: var(--accent);
  }
  .dialog-body :global(.dialog-footer) {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    margin-top: 20px;
  }
  .dialog-body :global(.dialog-hint) {
    color: var(--text2);
    font-size: 12px;
    line-height: 1.6;
    margin: 10px 0;
  }
  .dialog-body :global(.dialog-error) {
    color: var(--danger);
    font-size: 14px;
    margin: 12px 0;
    white-space: pre-wrap;
  }
  .dialog-body :global(.field) {
    display: flex;
    flex-direction: column;
    gap: 7px;
    font-size: 12px;
    margin: 14px 0;
  }
  .dialog-body :global(input[type='checkbox']) {
    accent-color: var(--accent);
  }
</style>
