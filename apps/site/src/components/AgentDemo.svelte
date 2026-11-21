<script lang="ts">
  import examples from '../content/demo.json';
  import manifest from '../assets/app-demo/manifest.json';
  import atlas from '../assets/app-demo/atlas.webp';
  import nova from '../assets/app-demo/nova.webp';
  import fern from '../assets/app-demo/fern.webp';

  const captures: Record<string, string> = {atlas, nova, fern};
  const regions: Record<string, {x: number; y: number; width: number; height: number}> = manifest.hotspots;
  let selected = $state(0);
</script>

<div class="agent-demo" id="explore" aria-labelledby="demo-title">
  <div class="demo-heading"><p id="demo-title">A few ways to work together</p><span>Native app captures</span></div>
  <div class="native-preview">
    <div class="capture">
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
  </div>
</div>

<style>
  .agent-demo { text-align: left; }
  .demo-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px 20px; margin-bottom: 16px; font-size: 12px; }
  .demo-heading p { margin: 0; font-weight: 600; }
  .demo-heading > span { color: var(--muted); }
  .capture { position: relative; width: 100%; }
  .capture img { display: block; width: 100%; height: auto; }
  .capture img[hidden] { display: none; }
  .hotspots { position: absolute; inset: 0; }
  .hotspots button { position: absolute; margin: 0; padding: 0; border: 0; border-radius: 0; background: transparent; appearance: none; cursor: pointer; }
  .hotspots button:focus-visible { outline: 2px solid var(--ink); outline-offset: 2px; }
  @media (max-width: 760px) {
    .hotspots { display: none; }
  }
</style>
