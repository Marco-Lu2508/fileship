<script>
  import { toasts } from '../stores/toast.js'
  import Icon from './Icon.svelte'

  const iconMap = { success: 'check', error: 'warning', info: 'info' }
</script>

<div class="toast-container" aria-live="polite">
  {#each $toasts as t (t.id)}
    <div class="toast {t.type}" role="alert">
      <span class="toast-icon"><Icon name={iconMap[t.type] || 'info'} size={14} /></span>
      <span class="toast-msg">{t.message}</span>
    </div>
  {/each}
</div>

<style>
  .toast-container {
    position: fixed; bottom: 1.25rem; right: 1.25rem;
    display: flex; flex-direction: column; gap: 0.4rem; z-index: 1000;
  }
  .toast {
    padding: 0.6rem 1rem; border-radius: 4px; font-size: 0.85rem;
    display: flex; align-items: center; gap: 0.5rem;
    animation: slide-in 0.15s ease; max-width: 340px;
    box-shadow: var(--shadow);
  }
  .toast.success { background: var(--surface); border: 1px solid var(--success); color: var(--success); }
  .toast.error   { background: var(--surface); border: 1px solid var(--danger);  color: var(--danger); }
  .toast.info    { background: var(--surface); border: 1px solid var(--border);  color: var(--text); }
  .toast-icon { display: flex; flex-shrink: 0; }
  .toast-msg { flex: 1; }
  @keyframes slide-in {
    from { transform: translateX(100%); opacity: 0; }
    to   { transform: translateX(0);    opacity: 1; }
  }
</style>
