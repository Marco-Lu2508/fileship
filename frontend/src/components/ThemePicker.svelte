<script>
  import { theme, THEMES } from '../stores/theme.js'
  import Icon from './Icon.svelte'

  export let onClose = () => {}
</script>

<div class="overlay" role="dialog" aria-modal="true" aria-label="Theme picker" tabindex="-1" onclick={(e) => { if (e.target === e.currentTarget) onClose() }} onkeydown={(e) => { if (e.key === 'Escape') onClose() }}>
  <div class="picker">
    <div class="picker-header">
      <span>Theme</span>
      <button onclick={onClose}><Icon name="x" size={14} /></button>
    </div>
    <div class="theme-list">
      {#each THEMES as t}
        <button
          class="theme-item"
          class:active={$theme === t.id}
          onclick={() => { theme.set(t.id); onClose() }}
          data-theme-preview={t.id}
        >
          <span class="swatch" data-theme={t.id}></span>
          <span>{t.label}</span>
          {#if $theme === t.id}
            <span class="active-mark"><Icon name="check" size={12} /></span>
          {/if}
        </button>
      {/each}
    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; z-index: 900;
    display: flex; align-items: flex-start; justify-content: flex-end;
    padding: 4.5rem 1rem 0;
  }
  .picker {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: 9px; width: 224px; box-shadow: var(--shadow);
    overflow: hidden;
  }
  .picker-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 0.8rem 0.9rem; border-bottom: 1px solid var(--border);
    font-size: 0.72rem; color: var(--text2); text-transform: uppercase; letter-spacing: 0.08em;
  }
  .picker-header button { background: none; border: none; color: var(--text2); cursor: pointer; display: flex; }
  .theme-list { padding: 0.3rem; }
  .theme-item {
    display: flex; align-items: center; gap: 0.7rem;
    width: 100%; background: none; border: none; color: var(--text);
    padding: 0.65rem 0.6rem; border-radius: 6px; cursor: pointer; font-size: 0.875rem;
    text-align: left;
  }
  .theme-item:hover { background: var(--row-hover); }
  .theme-item.active { color: var(--accent); }
  .active-mark { margin-left: auto; color: var(--accent); display: flex; }
  .swatch {
    width: 22px; height: 22px; border-radius: 6px; border: 1px solid var(--border); flex-shrink: 0;
  }
  /* Swatch Farben */
  .swatch[data-theme="dark"]      { background: #172235; }
  .swatch[data-theme="light"]     { background: #1565c0; }
  .swatch[data-theme="nord"]      { background: #88c0d0; }
  .swatch[data-theme="solarized"] { background: #268bd2; }
  .swatch[data-theme="gruvbox"]   { background: #d79921; }
</style>
