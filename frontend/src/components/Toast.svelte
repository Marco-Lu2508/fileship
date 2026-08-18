<script>
  import { toasts } from '../stores/toast.js'
</script>

<div class="toast-container">
  {#each $toasts as t (t.id)}
    <div class="toast {t.type}">
      {#if t.type === 'success'}✅{:else if t.type === 'error'}❌{:else}ℹ️{/if}
      {t.message}
    </div>
  {/each}
</div>

<style>
  .toast-container {
    position: fixed;
    bottom: 1.5rem;
    right: 1.5rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    z-index: 1000;
  }
  .toast {
    padding: 0.75rem 1.25rem;
    border-radius: 8px;
    font-size: 0.9rem;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    animation: slidein 0.2s ease;
    max-width: 360px;
    box-shadow: 0 4px 12px rgba(0,0,0,0.4);
  }
  .toast.success { background: #1a3a2a; border: 1px solid #2d6a4f; color: #6ee7b7; }
  .toast.error   { background: #3a1a1a; border: 1px solid #7f1d1d; color: #fca5a5; }
  .toast.info    { background: #1a1d3a; border: 1px solid #2d3a7f; color: #93c5fd; }
  @keyframes slidein {
    from { transform: translateX(100%); opacity: 0; }
    to   { transform: translateX(0);    opacity: 1; }
  }
</style>
