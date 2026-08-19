<script>
  import { loadFiles } from '../stores/files.js'
  import Icon from './Icon.svelte'

  export let path = ''
  export let onNavigate = (p) => loadFiles(p)

  $: parts = path ? path.split('/').filter(Boolean) : []

  function go(index) {
    onNavigate(parts.slice(0, index + 1).join('/'))
  }
</script>

<nav class="breadcrumb" aria-label="Path">
  <button onclick={() => onNavigate('')} class="crumb home" title="Home">
    <Icon name="home" size={14} />
    <span>Home</span>
  </button>
  {#each parts as part, i}
    <span class="sep"><Icon name="chevron_r" size={12} /></span>
    <button class="crumb" onclick={() => go(i)}>{part}</button>
  {/each}
</nav>

<style>
  .breadcrumb {
    display: flex;
    align-items: center;
    gap: 0.1rem;
    flex-wrap: wrap;
    min-width: 0;
  }
  .crumb {
    background: none;
    border: none;
    color: var(--text2);
    cursor: pointer;
    font-size: 0.85rem;
    padding: 0.2rem 0.4rem;
    border-radius: 3px;
    display: flex;
    align-items: center;
    gap: 0.3rem;
    white-space: nowrap;
  }
  .crumb:hover { background: var(--border); color: var(--text); }
  .sep { color: var(--border); display: flex; align-items: center; }
</style>
