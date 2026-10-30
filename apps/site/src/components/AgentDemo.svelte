<script lang="ts">
  import examples from '../content/demo.json';
  import manifest from '../assets/app-demo/manifest.json';
  import atlas from '../assets/app-demo/atlas.webp';
  import nova from '../assets/app-demo/nova.webp';
  import fern from '../assets/app-demo/fern.webp';

  const captures: Record<string, string> = {atlas, nova, fern};
  const regions: Record<string, {x: number; y: number; width: number; height: number}> = manifest.hotspots;
  let selected = $state(0);
  const agent = $derived(examples[selected]);
  let dialog: HTMLDialogElement;
  let zoomButton: HTMLButtonElement;
</script>

{#snippet surface(expanded = false)}
  <div class="capture" class:expanded style:--native-width={`${manifest.width / manifest.scale}px`}>
    {#each examples as example, index (example.id)}
      <img
        data-native-capture={example.id}
        src={captures[example.id]}
        width={manifest.width}
        height={manifest.height}
        alt={`Polymux for macOS with ${example.name} selected. Captured from the native app, including its Liquid Glass controls.`}
        hidden={selected !== index}
        draggable="false"
      />
    {/each}
    <div class="hotspots" role="group" aria-label="Conversations in the app preview">
      {#each examples as example, index (example.id)}
        {@const region = regions[example.id]}
        <button
          type="button"
          aria-label={`Open ${example.name}`}
          aria-pressed={selected === index}
          style:left={`${region.x / manifest.width * 100}%`}
          style:top={`${region.y / manifest.height * 100}%`}
          style:width={`${region.width / manifest.width * 100}%`}
          style:height={`${region.height / manifest.height * 100}%`}
          onclick={() => selected = index}
        ></button>
      {/each}
    </div>
  </div>
{/snippet}

<div class="agent-demo" id="explore" aria-labelledby="demo-title">
  <div class="demo-heading"><p id="demo-title">A few ways to work together</p><span>Native app captures</span></div>
  <div class="native-preview">{@render surface()}</div>
  <div class="preview-tools">
    <div class="agent-choices" role="group" aria-label="Example agents">
      {#each examples as example, index (example.id)}
        <button type="button" class:selected={selected === index} aria-pressed={selected === index} onclick={() => selected = index}>{example.name}</button>
      {/each}
    </div>
    <button class="zoom-button" type="button" aria-haspopup="dialog" aria-controls="capture-dialog" bind:this={zoomButton} onclick={() => dialog.showModal()}>Zoom</button>
  </div>
  <p class="demo-note">Real app renders with example data. Nothing is sent. Zoom to view at native size.</p>
  <div class="demo-caption">
    <div><h2>{agent.title}</h2><p>{agent.description}</p></div>
    <a class="demo-guide" href={agent.guide}>Explore <span aria-hidden="true">↗</span></a>
  </div>
  <details class="transcript">
    <summary>Transcript</summary>
    <div aria-live="polite" aria-atomic="true"><h3>You</h3><p>{agent.prompt}</p><h3>{agent.name}</h3><p>{agent.reply}</p></div>
  </details>
</div>

<dialog class="capture-dialog" id="capture-dialog" aria-label="Native app preview" bind:this={dialog} onclose={() => zoomButton.focus({preventScroll: true})}>
  <header><strong>{agent.name} · Native app capture</strong><button type="button" onclick={() => dialog.close()}>Close</button></header>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex (Keyboard users must be able to scroll the native-size capture.) -->
  <div class="zoom-viewport" tabindex="0" role="region" aria-label="Native-size app capture. Scroll to explore.">{@render surface(true)}</div>
</dialog>

<style>
  .agent-demo { text-align: left; }
  .demo-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px 20px; margin-bottom: 16px; font-size: 12px; }
  .demo-heading p { margin: 0; font-weight: 600; }
  .demo-heading > span, .demo-note { color: var(--muted); }
  .capture { position: relative; width: 100%; }
  .capture img { display: block; width: 100%; height: auto; }
  .capture img[hidden] { display: none; }
  .hotspots { position: absolute; inset: 0; }
  .hotspots button { position: absolute; margin: 0; padding: 0; border: 0; border-radius: 0; background: transparent; appearance: none; cursor: pointer; }
  .hotspots button:focus-visible { outline: 2px solid var(--ink); outline-offset: 2px; }
  .preview-tools { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-top: 16px; }
  .agent-choices { display: flex; gap: 8px; }
  .preview-tools button, .capture-dialog header button { min-height: 44px; padding: 0 18px; border: 1px solid var(--focus); border-radius: 8px; color: var(--ink); background: transparent; font: inherit; font-size: 12px; font-weight: 600; cursor: pointer; }
  .preview-tools button:hover, .capture-dialog header button:hover { background: var(--soft); }
  .agent-choices button.selected { color: var(--on-accent); background: var(--accent); border-color: var(--accent); }
  .demo-note { margin: 14px 0 0; font-size: 12px; line-height: 1.65; }
  .demo-caption { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; min-height: 130px; padding: 28px 0 0; }
  .demo-caption h2 { margin: 0; font-size: 20px; line-height: 1.3; font-weight: 550; letter-spacing: -.025em; }
  .demo-caption p { max-width: 510px; margin: 10px 0 0; color: var(--muted); font-size: 13px; line-height: 1.65; }
  .demo-guide { display: inline-flex; align-items: center; justify-content: center; gap: 16px; min-height: 44px; padding: 0 16px; border: 1px solid var(--focus); border-radius: 8px; font-size: 12px; font-weight: 600; }
  .demo-guide:hover { background: var(--soft); }
  .transcript { border-top: 1px solid var(--line); }
  .transcript summary { display: flex; align-items: center; min-height: 44px; font-size: 12px; cursor: pointer; }
  .transcript summary:focus-visible { outline: 2px solid var(--ink); outline-offset: 2px; }
  .transcript h3 { margin: 20px 0 8px; font-size: 13px; }
  .transcript p { max-width: 680px; margin: 0 0 20px; color: var(--text); font-size: 14px; line-height: 1.7; white-space: pre-wrap; }
  .capture-dialog { width: fit-content; max-width: calc(100vw - 24px); max-height: calc(100dvh - 24px); padding: 0; border: 1px solid var(--focus); border-radius: var(--radius); color: var(--ink); background: var(--page); }
  .capture-dialog::backdrop { background: var(--scrim); }
  .capture-dialog header { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 12px 16px; border-bottom: 1px solid var(--line); font-size: 13px; }
  .zoom-viewport { max-height: calc(100dvh - 100px); overflow: auto; }
  .capture.expanded { width: var(--native-width); }
  @media (max-width: 760px) {
    .native-preview .hotspots { display: none; }
    .preview-tools { gap: 12px; }
    .agent-choices { flex: 1; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 6px; }
    .preview-tools button { padding-inline: 12px; }
    .agent-choices button { padding-inline: 0; }
    .demo-caption { flex-direction: column; gap: 16px; min-height: 200px; padding-top: 24px; }
    .demo-caption h2 { font-size: 19px; }
  }
</style>
