<script>
  import { onMount } from 'svelte'
  export let label
  export let width = '620px'
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
  aria-label={label}
  style:--modal-width={width}
  oncancel={(event) => {
    event.preventDefault()
    if (!busy) onClose()
  }}
>
  <slot />
</dialog>

<style>
  dialog {
    margin: auto;
    padding: 0;
    width: min(var(--modal-width), calc(100vw - 32px));
    max-height: calc(100dvh - 32px);
    border: 0;
    border-radius: var(--radius-lg);
    background: var(--surface);
    color: var(--text);
    box-shadow: var(--shadow-lg);
    overflow: auto;
    overscroll-behavior: contain;
  }
  dialog::backdrop {
    background: #0b101b99;
    backdrop-filter: blur(4px);
  }
</style>
