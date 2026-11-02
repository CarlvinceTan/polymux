<script lang="ts">
  import {tick} from 'svelte';
  import {fade} from 'svelte/transition';
  import Icon from '../../shared/components/Icon.svelte';
  import {MAIN_UI_ICON_SIZE, MAIN_UI_ICON_STROKE_WIDTH} from '../../shared/layout/iconSizing';
  import {MENU_EDGE_MARGIN, clampToMenuEdge} from '../../shared/layout/menuPlacement';
  import {scrollFade} from '../../shared/scrollFade';

  export let memories: string[];
  let open = false;
  let trigger: HTMLButtonElement;
  let panel: HTMLDivElement;
  let left = 0;
  let top = 0;
  let maxHeight = 240;
  let placed = false;

  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return {destroy: () => node.remove()};
  }

  async function toggle() {
    open = !open;
    if (!open) return;
    placed = false;
    await tick();
    if (!open || !panel) return;
    const anchor = trigger.getBoundingClientRect();
    const box = panel.getBoundingClientRect();
    const below = window.innerHeight - anchor.bottom - MENU_EDGE_MARGIN - 6;
    const above = anchor.top - MENU_EDGE_MARGIN - 6;
    const down = box.height <= below || below >= above;
    maxHeight = Math.max(0, Math.min(240, down ? below : above));
    left = clampToMenuEdge(anchor.left + anchor.width / 2 - box.width / 2, box.width, window.innerWidth);
    top = down ? anchor.bottom + 6 : anchor.top - 6 - Math.min(box.height, maxHeight);
    placed = true;
    panel.focus({preventScroll: true});
  }

  function dismiss(event: PointerEvent) {
    if (open && event.target instanceof Node && !panel?.contains(event.target) && !trigger?.contains(event.target)) open = false;
  }

  function keydown(event: KeyboardEvent) {
    if (open && event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      open = false;
      trigger.focus({preventScroll: true});
    }
  }
</script>

<svelte:window onpointerdowncapture={dismiss} onkeydown={keydown} onresize={() => open = false} onscroll={() => open = false} onwheel={(event) => { if (!panel?.contains(event.target as Node)) open = false; }}/>
<span class="message-action-wrap memory-action">
  <button bind:this={trigger} type="button" class:active={open} aria-label="Memories cited" aria-haspopup="dialog" aria-expanded={open} onclick={toggle}>
    <Icon name="memory-cited" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/>
  </button>
</span>
{#if open}
  <div bind:this={panel} use:portal use:scrollFade transition:fade={{duration: 120}} class="polymux-dropdown-menu memory-citations" role="dialog" aria-label="Memories cited" tabindex="-1" style:left={`${left}px`} style:top={`${top}px`} style:max-height={`${maxHeight}px`} style:visibility={placed ? 'visible' : 'hidden'}>
    <h3>Memories cited</h3>
    <ul>{#each memories as memory}<li>{memory}</li>{/each}</ul>
  </div>
{/if}

<style>
  .memory-action > button:hover, .memory-action > button:focus-visible { background: transparent; }
  .memory-action > button:focus-visible { outline: 2px solid currentColor; outline-offset: 2px; }
  .memory-citations { position: fixed; z-index: 200; box-sizing: border-box; width: max-content; max-width: min(420px, calc(100vw - 24px)); padding: 12px 16px; overflow-y: auto; overscroll-behavior: contain; scrollbar-width: none; outline: none; }
  .memory-citations::-webkit-scrollbar { display: none; }
  h3 { margin: 0 0 8px; font-size: 13px; font-weight: 500; color: var(--neutral-950); }
  ul { margin: 0; padding-left: 16px; color: var(--neutral-500); font-size: 13px; line-height: 1.5; }
  li { overflow-wrap: anywhere; }
  li + li { margin-top: 6px; }
</style>
