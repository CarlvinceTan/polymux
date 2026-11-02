<script lang="ts">
  import { onMount } from 'svelte';
  let { level }: { level: number } = $props();
  let width = $state(0);
  let samples: number[] = $state([]);
  let envelope = 0;
  const count = $derived(Math.max(1, Math.ceil(width / 5.5)));

  onMount(() => {
    const timer = setInterval(() => {
      const target = Math.sqrt(Math.max(0, (Math.max(0, Math.min(1, level)) - .02) / .98));
      envelope += (target - envelope) * (target > envelope ? .85 : .5);
      samples = [envelope, ...samples].slice(0, count);
    }, 60);
    return () => clearInterval(timer);
  });
</script>

<div class="wave" bind:clientWidth={width} role="img" aria-label="Recording">
  {#each Array.from({ length: count }, (_, i) => i) as index (index)}
    <span style:height="{3 + 15 * (samples[index] ?? 0)}px"></span>
  {/each}
</div>

<style>
  .wave { position: absolute; inset: 0 15px; display: flex; align-items: center; gap: 2.5px; overflow: hidden; pointer-events: none; }
  span { width: 3px; flex: none; border-radius: 3px; background: var(--text); }
  @media (prefers-reduced-motion: no-preference) { span { transition: height 60ms linear; } }
</style>
