<script>
  import { loadFiles } from '../stores/files.js'

  export let path = ''

  $: parts = path ? path.split('/').filter(Boolean) : []

  function navigateTo(index) {
    const target = parts.slice(0, index + 1).join('/')
    loadFiles(target)
  }
</script>

<nav class="breadcrumb">
  <button onclick={() => loadFiles('')}>🏠 Home</button>
  {#each parts as part, i}
    <span class="sep">/</span>
    <button onclick={() => navigateTo(i)}>{part}</button>
  {/each}
</nav>

<style>
  .breadcrumb {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.5rem 0;
    flex-wrap: wrap;
  }
  button {
    background: none;
    border: none;
    color: #a0aec0;
    cursor: pointer;
    font-size: 0.9rem;
    padding: 0.2rem 0.4rem;
    border-radius: 4px;
  }
  button:hover { background: #2a2d3a; color: #fff; }
  .sep { color: #4a5568; }
</style>
