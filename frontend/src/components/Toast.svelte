<script>
  import { toasts } from '../stores/toast.js'
  import Icon from './Icon.svelte'

  const iconMap = { success: 'check', error: 'warning', info: 'info' }
</script>

<div class="toast-container" aria-live="polite" aria-atomic="false">
  {#each $toasts as t (t.id)}
    <div class="toast {t.type}" role="alert">
      <span class="toast-icon {t.type}">
        <Icon name={iconMap[t.type] || 'info'} size={13} />
      </span>
      <span class="toast-msg">{t.message}</span>
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
    z-index: 1200;
    pointer-events: none;
  }

  .toast {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.65rem 1rem 0.65rem 0.75rem;
    border-radius: var(--radius);
    font-size: 0.85rem;
    font-weight: 500;
    background: var(--surface);
    border: 1px solid var(--border);
    color: var(--text);
    box-shadow: var(--shadow);
    max-width: 360px;
    animation: toast-in 0.2s cubic-bezier(.2,.8,.3,1) both;
    pointer-events: auto;
  }

  @keyframes toast-in {
    from { opacity: 0; transform: translateX(20px) scale(0.95); }
    to   { opacity: 1; transform: none; }
  }

  .toast-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    flex-shrink: 0;
  }

  .toast-icon.success { background: var(--success-bg); color: var(--success); }
  .toast-icon.error   { background: var(--danger-bg);  color: var(--danger); }
  .toast-icon.info    { background: var(--accent-soft); color: var(--accent); }

  .toast.success { border-color: var(--success); }
  .toast.error   { border-color: var(--danger); }

  .toast-msg { flex: 1; line-height: 1.4; }
</style>
