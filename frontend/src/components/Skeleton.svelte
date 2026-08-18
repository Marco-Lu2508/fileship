<script>
  export let rows = 8
</script>

<div class="skeleton-wrap">
  {#each Array(rows) as _, i}
    <div class="skeleton-row">
      <div class="skel check"></div>
      <div class="skel name" style="width: {40 + (i * 17) % 35}%"></div>
      <div class="skel size"></div>
      <div class="skel date"></div>
      <div class="skel actions"></div>
    </div>
  {/each}
</div>

<style>
  .skeleton-wrap { padding: 0.25rem 0; }
  .skeleton-row {
    display: flex; align-items: center; gap: 0.75rem;
    padding: 0.7rem 0.75rem; border-bottom: 1px solid var(--border);
  }
  .skel {
    background: var(--skel-bg);
    border-radius: 4px;
    height: 14px;
    animation: shimmer 1.4s infinite;
  }
  .check  { width: 1.2rem; flex-shrink: 0; }
  .name   { flex: 1; }
  .size   { width: 4rem; flex-shrink: 0; }
  .date   { width: 6rem; flex-shrink: 0; }
  .actions { width: 7rem; flex-shrink: 0; }

  @keyframes shimmer {
    0%   { opacity: 0.4; }
    50%  { opacity: 0.8; }
    100% { opacity: 0.4; }
  }
</style>
