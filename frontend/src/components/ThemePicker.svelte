<script>
  import { translator as i18n } from '../stores/i18n.js'
  import { theme, THEMES } from '../stores/theme.js'
  import Dialog from './Dialog.svelte'
  import Icon from './Icon.svelte'
  export let onClose = () => {}
</script>

<Dialog title={$i18n('Appearance')} {onClose}>
  <p class="intro">
    {$i18n('Choose your design. Changes are saved automatically.')}
  </p>
  <div class="designs">
    {#each [...THEMES].sort( (a, b) => (a.id === 'light' ? -1 : b.id === 'light' ? 1 : 0) ) as design}
      <button
        class="design"
        class:active={$theme === design.id}
        aria-pressed={$theme === design.id}
        onclick={() => theme.set(design.id)}
      >
        <span class="preview" data-design={design.id} aria-hidden="true">
          <span class="rail"><i></i><i></i><i></i><i></i></span>
          <span class="canvas"
            ><span class="preview-heading"></span><span class="preview-tools"
            ></span><span class="preview-row"></span><span class="preview-row"
            ></span><span class="preview-row"></span></span
          >
        </span>
        <span class="design-label"
          >{$i18n(design.label)}
          {#if $theme === design.id}<Icon name="check" size={17} />{/if}
        </span>
      </button>
    {/each}
  </div>
  <div class="dialog-footer">
    <button class="dialog-primary" onclick={onClose}>{$i18n('Done')}</button>
  </div>
</Dialog>

<style>
  .intro {
    color: var(--text2);
    margin-bottom: 20px;
    font-size: 14px;
  }
  .designs {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 14px;
  }
  .designs .design {
    display: flex;
    flex-direction: column;
    padding: 5px;
    min-width: 0;
    background: var(--surface);
    border: 2px solid var(--border);
    border-radius: 10px;
    text-align: left;
  }
  .designs .design:hover {
    border-color: var(--text3);
  }
  .designs .design.active {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-soft);
  }
  .preview {
    --paper: #ffffff;
    --ink: #234232;
    --highlight: #d2e5c3;
    --rule: #dde5df;
    display: flex;
    width: 100%;
    height: 94px;
    border-radius: 5px;
    overflow: hidden;
    background: var(--paper);
  }
  .preview[data-design='ocean'] {
    --ink: #142e52;
    --highlight: #b9d5ff;
    --rule: #d9e3f0;
  }
  .preview[data-design='violet'] {
    --ink: #372447;
    --highlight: #dfc6ff;
    --rule: #e4deed;
  }
  .preview[data-design='dark'] {
    --paper: #1d2531;
    --ink: #111823;
    --highlight: #aacafe;
    --rule: #354152;
  }
  .preview[data-design='nord'] {
    --paper: #2e3440;
    --ink: #202834;
    --highlight: #a3d6e1;
    --rule: #465267;
  }
  .preview[data-design='solarized'] {
    --paper: #073642;
    --ink: #001e28;
    --highlight: #8ad9d5;
    --rule: #205461;
  }
  .preview[data-design='gruvbox'] {
    --paper: #282828;
    --ink: #191c1b;
    --highlight: #e9c783;
    --rule: #49453d;
  }
  .rail {
    width: 29%;
    background: var(--ink);
    padding: 14px 6px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .rail i {
    height: 4px;
    width: 70%;
    border-radius: 2px;
    background: var(--highlight);
    opacity: 0.4;
  }
  .rail i:nth-child(2) {
    width: 100%;
    height: 10px;
    opacity: 1;
  }
  .canvas {
    padding: 12px 8px;
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 7px;
  }
  .preview-heading {
    width: 65%;
    height: 5px;
    background: var(--highlight);
    border-radius: 2px;
  }
  .preview-tools {
    width: 80%;
    height: 3px;
    background: var(--rule);
  }
  .preview-row {
    height: 9px;
    border-bottom: 1px solid var(--rule);
  }
  .design-label {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 5px;
    padding: 9px 5px 5px;
    font-weight: 500;
    width: 100%;
  }
  .design-label :global(svg) {
    color: var(--accent);
    flex-shrink: 0;
  }
  @media (max-width: 480px) {
    .designs {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
  }
</style>
