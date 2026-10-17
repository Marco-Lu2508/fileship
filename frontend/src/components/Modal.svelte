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
    opacity: 0;
    transform: scale(0.97) translateY(8px);
    transition:
      opacity 0.2s cubic-bezier(0.16, 1, 0.3, 1),
      transform 0.2s cubic-bezier(0.16, 1, 0.3, 1),
      display 0.2s allow-discrete,
      overlay 0.2s allow-discrete;
  }
  dialog[open] {
    opacity: 1;
    transform: scale(1) translateY(0);
  }
  @starting-style {
    dialog[open] {
      opacity: 0;
      transform: scale(0.97) translateY(8px);
    }
  }
  dialog::backdrop {
    background: #0b101b99;
    backdrop-filter: blur(6px);
    opacity: 0;
    transition:
      opacity 0.2s cubic-bezier(0.16, 1, 0.3, 1),
      display 0.2s allow-discrete,
      overlay 0.2s allow-discrete;
  }
  dialog[open]::backdrop {
    opacity: 1;
  }
  @starting-style {
    dialog[open]::backdrop {
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    dialog,
    dialog::backdrop {
      transition: none;
      transform: none;
    }
  }
</style>
