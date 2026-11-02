<script module lang="ts">
  /** One way of opening something. The glyph name is reached for inline
   * because the instance script imports `Icon` as a value under that name. */
  export type OpenChoice = {
    value: string;
    label: string;
    danger?: boolean;
    disabled?: boolean;
    separatorBefore?: boolean;
    /** A glyph, for the ways that are the app's own. */
    icon?: import('svelte').ComponentProps<import('./Icon.svelte').default>['name'];
    /**
     * An application's own icon, as a data url. Takes precedence over `icon`:
     * "Open in Helium" is recognisable as Helium before the words are read.
     */
    image?: string | null;
  };

  /**
   * Where the menu goes.
   *
   * `point` is a click: the menu's top-left corner lands on it, which is what
   * a menu summoned by pointing at something looks like everywhere else.
   * `rect` is a piece of the page — a link in a message — and the menu hangs
   * directly under it, left edges aligned, because the link is still on screen
   * and the menu belongs to it visibly rather than to wherever the pointer
   * happened to be inside the words.
   */
  export type OpenAnchor = {point: {x: number; y: number}} | {rect: DOMRect};

  /** Under a link, and over the point of a click. */
  const GAP = 6;
</script>

<script lang="ts">
  import Icon from './Icon.svelte';
  import {MENU_EDGE_MARGIN, clampToMenuEdge} from '../layout/menuPlacement';

  export let choices: OpenChoice[] = [];
  export let anchor: OpenAnchor | null = null;
  export let onChoose: (value: string) => void = () => {};
  export let onClose: () => void = () => {};
  export let compact = false;
  export let ariaLabel = '';

  let menu: HTMLDivElement | null = null;
  let left = 0;
  let top = 0;
  /** Held back until measured: a menu painted at 0,0 and then moved is a jump. */
  let placed = false;

  /** Kept at body level so viewport coordinates stay viewport coordinates and
   * a transformed or overflow-clipped drawer cannot hide the menu. */
  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return {destroy: () => node.remove()};
  }

  /**
   * Placement, once the menu has a size.
   *
   * The preference is stated in one place and the correction in another: put
   * it where it was asked for, then move it only as far as the window
   * requires. Flipping above the anchor is for the case where sliding up would
   * cover the very thing the menu is about — a link under the pointer, a row
   * being opened — which is worse than opening upwards.
   */
  function place(): void {
    if (!menu || !anchor) return;
    const {width, height} = menu.getBoundingClientRect();

    let x: number;
    let y: number;
    let flipped: number;
    if ('rect' in anchor) {
      x = anchor.rect.left;
      y = anchor.rect.bottom + GAP;
      // Above the link rather than over it.
      flipped = anchor.rect.top - GAP - height;
    } else {
      x = anchor.point.x;
      y = anchor.point.y + GAP;
      flipped = anchor.point.y - GAP - height;
    }

    if (y + height > window.innerHeight - MENU_EDGE_MARGIN && flipped >= MENU_EDGE_MARGIN) y = flipped;
    left = clampToMenuEdge(x, width, window.innerWidth);
    top = clampToMenuEdge(y, height, window.innerHeight);
    placed = true;
  }

  /** Re-measured whenever what is being placed changes. */
  $: if (menu && anchor && choices.length) void Promise.resolve().then(place);

  function keydown(event: KeyboardEvent): void {
    if (anchor && choices.length && event.key === 'Escape') {
      event.stopPropagation();
      onClose();
    }
  }

  /** Focus lands inside so Escape and the arrow keys reach the menu, and so
   * dismissing it returns the page to a sensible place. */
  function mounted(node: HTMLDivElement) {
    node.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus({preventScroll: true});
    return {};
  }
</script>

<svelte:window on:resize={() => onClose()} on:keydown={keydown}/>

{#if anchor && choices.length}
  <!-- Dismissed by anything outside it, including a scroll: the menu is about
       one thing on the page, and once that thing has moved it is about nothing. -->
  <div
    use:portal
    class="open-menu-shade"
    role="presentation"
    onpointerdown={() => onClose()}
    onwheel={() => onClose()}
  ></div>
  <div
    use:portal
    bind:this={menu}
    use:mounted
    class="polymux-dropdown-menu open-menu"
    class:compact
    class:placed
    style="left: {left}px; top: {top}px"
    role="menu"
    aria-label={ariaLabel || undefined}
    tabindex="-1"
    onkeydown={keydown}
  >
    {#each choices as choice (choice.value)}
      {#if choice.separatorBefore}<div class="separator" role="separator"></div>{/if}
      <button
        type="button"
        class="polymux-dropdown-item"
        class:danger={choice.danger}
        disabled={choice.disabled}
        role="menuitem"
        onclick={() => onChoose(choice.value)}
      >
        {#if choice.image}
          <img class="open-menu-app-icon" src={choice.image} alt=""/>
        {:else if choice.icon}
          <Icon name={choice.icon} size={14}/>
        {/if}
        <span>{choice.label}</span>
      </button>
    {/each}
  </div>
{/if}

<style>
  .polymux-dropdown-item:disabled { opacity: .45; }
  .separator { height: 1px; margin: 4px 6px; background: var(--neutral-200); }
</style>
