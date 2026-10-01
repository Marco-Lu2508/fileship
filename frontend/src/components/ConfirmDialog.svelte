<script>
  import { onMount } from 'svelte'
  import Icon from './Icon.svelte'
  export let title
  export let description
  export let confirmLabel = 'Confirm'
  export let busy = false
  export let onConfirm = () => {}
  export let onClose = () => {}
  let dialog
  onMount(() => {
    dialog.showModal()
  })
</script>

<dialog
  bind:this={dialog}
  aria-labelledby="confirm-title"
  aria-describedby="confirm-description"
  oncancel={(e) => {
    e.preventDefault()
    if (!busy) onClose()
  }}
>
  <span class="dialog-icon"><Icon name="trash" size={24} /></span>
  <h2 id="confirm-title">{title}</h2>
  <p id="confirm-description">{description}</p>
  <div class="buttons">
    <button disabled={busy} onclick={onClose}>Cancel</button><button
      class="confirm"
      disabled={busy}
      onclick={onConfirm}>{busy ? 'Moving…' : confirmLabel}</button
    >
  </div>
</dialog>

<style>
  dialog {
    margin: auto;
    width: min(420px, calc(100% - 32px));
    background: var(--surface);
    color: var(--text);
    padding: 27px;
    border: 1px solid var(--border);
    border-radius: 14px;
    box-shadow: var(--shadow-lg);
  }
  dialog::backdrop {
    background: #12221970;
    backdrop-filter: blur(3px);
  }
  .dialog-icon {
    display: grid;
    place-items: center;
    width: 47px;
    height: 47px;
    border-radius: 12px;
    background: var(--danger-bg);
    color: var(--danger);
    margin-bottom: 17px;
  }
  h2 {
    font-size: 20px;
    line-height: 1.4;
    font-weight: 600;
    letter-spacing: -0.5px;
  }
  p {
    color: var(--text2);
    font-size: 13px;
    margin: 10px 0 25px;
  }
  .buttons {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
  }
  button {
    border: 1px solid var(--border);
    border-radius: 7px;
    background: var(--surface);
    color: var(--text);
    padding: 10px 14px;
    cursor: pointer;
    font-size: 12px;
  }
  button:hover {
    background: var(--row-hover);
  }
  button.confirm {
    background: var(--danger);
    border-color: var(--danger);
    color: white;
  }
  button:disabled {
    opacity: 0.6;
    cursor: default;
  }
</style>
