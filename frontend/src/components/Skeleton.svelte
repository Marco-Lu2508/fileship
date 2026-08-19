<script>
  export let rows = 8
</script>

<div class="skeleton-wrap" aria-hidden="true">
  {#each Array(rows) as _, i}
    <div class="skeleton-row" style="animation-delay: {i * 40}ms">
      <div class="skel check"></div>
      <div class="skel icon"></div>
      <div class="skel name" style="width:{30 + (i * 17 + 7) % 45}%"></div>
      <div class="skel size"></div>
      <div class="skel date hide-mobile"></div>
      <div class="skel actions"></div>
    </div>
  {/each}
</div>

<style>
  .skeleton-wrap { padding: 0; }

  .skeleton-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border2);
    animation: row-in 0.25s ease both;
  }

  @keyframes row-in {
    from { opacity: 0; transform: translateX(-6px); }
    to   { opacity: 1; transform: none; }
  }

  .skel {
    background: var(--skel);
    border-radius: 4px;
    height: 12px;
    animation: pulse 1.6s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.3; }
    50%       { opacity: 0.65; }
  }

  .check   { width: 14px; flex-shrink: 0; border-radius: 3px; height: 14px; }
  .icon    { width: 16px; height: 16px; border-radius: 4px; flex-shrink: 0; }
  .name    { flex: 1; }
  .size    { width: 4rem; flex-shrink: 0; }
  .date    { width: 6rem; flex-shrink: 0; }
  .actions { width: 7rem; flex-shrink: 0; border-radius: 6px; height: 22px; }

  @media (max-width: 640px) { .hide-mobile { display: none; } }
</style>
