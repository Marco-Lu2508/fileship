<script>
  export let rows = 8
</script>

<div class="skeleton-wrap" aria-hidden="true">
  {#each Array(rows) as _, i}
    <div class="skeleton-row">
      <div class="skel check"></div>
      <div class="skel icon"></div>
      <div class="skel name" style="width:{35 + (i * 13) % 40}%"></div>
      <div class="skel size"></div>
      <div class="skel date"></div>
      <div class="skel actions"></div>
    </div>
  {/each}
</div>

<style>
  .skeleton-wrap { padding: 0; }
  .skeleton-row {
    display: flex; align-items: center; gap: 0.75rem;
    padding: 0.55rem 0.75rem; border-bottom: 1px solid var(--border);
  }
  .skel {
    background: var(--skel); border-radius: 3px; height: 12px;
    animation: pulse 1.5s ease-in-out infinite;
  }
  .check   { width: 1rem; flex-shrink: 0; }
  .icon    { width: 1rem; flex-shrink: 0; }
  .name    { flex: 1; }
  .size    { width: 3.5rem; flex-shrink: 0; }
  .date    { width: 5.5rem; flex-shrink: 0; }
  .actions { width: 6rem; flex-shrink: 0; }
  @keyframes pulse {
    0%, 100% { opacity: 0.35; }
    50%       { opacity: 0.7; }
  }
</style>
