<script lang="ts">
  import {onMount} from 'svelte';
  import type {TocItem} from '../../lib/content/types';

  let {items}: {items: TocItem[]} = $props();
  let active = $state('');
  onMount(() => {
    let frame = 0;
    const update = () => {
      const offset = (document.querySelector('.site-header')?.getBoundingClientRect().height ?? 68) + 32;
      let current = items[0]?.id ?? '';
      for (const item of items) {
        if ((document.getElementById(item.id)?.getBoundingClientRect().top ?? Infinity) <= offset) current = item.id;
        else break;
      }
      active = current;
      frame = 0;
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    window.addEventListener('scroll', schedule, {passive: true});
    window.addEventListener('resize', schedule);
    update();
    return () => {
      window.removeEventListener('scroll', schedule);
      window.removeEventListener('resize', schedule);
      if (frame) cancelAnimationFrame(frame);
    };
  });
</script>

<div class="page-outline">
  <h2>On this page</h2>
  <nav aria-label="On this page">
    {#each items as item (item.id)}
      <a class:active={active === item.id} class:nested={item.level === 3} href={`#${item.id}`} aria-current={active === item.id ? 'location' : undefined}>{item.text}</a>
    {/each}
  </nav>
</div>
