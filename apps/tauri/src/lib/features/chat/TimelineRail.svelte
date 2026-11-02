<script module lang="ts">
  export type TimelineItem = {
    id: string;
    /** The prompt that opened the turn — the peek's single title line. */
    prompt: string;
    /** The reply it drew, rendered as markdown and clamped in the peek. */
    reply: string;
  };

  /** Line widths by distance from the pointer; anything further rests at 6px. */
  export const HOVER_WIDTHS = [26, 20, 14, 9];

  /**
   * The fewest turns worth railing. The hover shape reaches HOVER_WIDTHS.length
   * points either side of the pointer, so a shorter rail can never draw the full
   * curve — it stays a stub whatever the reader points at.
   */
  export const TIMELINE_RAIL_MINIMUM = HOVER_WIDTHS.length * 2 - 1;
</script>

<script lang="ts">
  import {afterUpdate, onDestroy, onMount} from 'svelte';
  import {renderMarkdown} from './markdown';
  import {t} from '../../../i18n';

  /** Enough source for four rendered lines; parsing the whole reply per hover is waste. */
  const PEEK_SOURCE_LIMIT = 600;
  /** Half the peek's own height, used to keep it inside the viewport near the rail's ends. */
  const PEEK_HALF_HEIGHT = 84;

  export let items: TimelineItem[] = [];

  let activeId = '';
  let hoveredIndex: number | null = null;
  let frame = 0;
  let profiles: Array<{width: number; opacity: number; near: boolean}> = [];
  let peekHtml = '';
  let peekShift = 0;

  $: profiles = buildProfiles(items, hoveredIndex, activeId);
  // Only the hovered turn is parsed: markdown for every point would run the
  // whole conversation through marked on each render.
  $: peekHtml = hoveredIndex === null
    ? ''
    : renderMarkdown((items[hoveredIndex]?.reply ?? '').slice(0, PEEK_SOURCE_LIMIT));

  /** A pixel of slack, so sub-pixel rounding on the last line cannot pass for
      overflow and cost the reader a word that was already fully visible. */
  function fits(node: HTMLElement): boolean {
    return node.scrollHeight <= node.clientHeight + 1;
  }

  /**
   * Trims the rendered reply to the height CSS allows, ending it in an ellipsis.
   *
   * A CSS line clamp cannot do this job: across the block children markdown
   * emits — headings, paragraphs, list items — it counts lines inconsistently
   * and lets a fifth one through, and it draws no ellipsis when the overflow
   * falls on a block boundary rather than mid-line. Dropping trailing words
   * until the content fits is exact, and the ellipsis is then a real character
   * sitting at the end of the last line that survived.
   */
  function clampToBox(node: HTMLElement): void {
    if (fits(node)) return;

    const texts: Text[] = [];
    const walker = document.createTreeWalker(node, NodeFilter.SHOW_TEXT);
    for (let text = walker.nextNode(); text; text = walker.nextNode()) texts.push(text as Text);
    // Split on whitespace but keep it, so joining a prefix restores the spacing.
    const words = texts.map((text) => text.data.split(/(\s+)/));
    const total = words.reduce((count, parts) => count + parts.length, 0);

    /**
     * Keeps the first `budget` words and ends them in an ellipsis. Every step is
     * reversible — blocks emptied by the trim are hidden rather than removed, so
     * a later, larger budget can bring them back. Removing them would detach the
     * very text nodes the search writes to, and the search would then be blind
     * to its own changes.
     */
    const apply = (budget: number): void => {
      let left = budget;
      texts.forEach((text, index) => {
        const take = Math.max(0, Math.min(words[index].length, left));
        left -= take;
        text.data = words[index].slice(0, take).join('');
      });
      const last = texts.filter((text) => text.data.trim()).pop();
      if (last) last.data = `${last.data.replace(/\s+$/, '')}…`;
      for (const element of node.querySelectorAll('*')) {
        const blank = !element.textContent?.trim() && !element.querySelector('img,code');
        element.classList.toggle('peek-blank', blank);
      }
    };

    // The largest prefix that fits with its ellipsis, in a handful of reflows
    // rather than one per word.
    let low = 0;
    let high = total - 1;
    while (low < high) {
      const middle = Math.ceil((low + high) / 2);
      apply(middle);
      if (fits(node)) low = middle;
      else high = middle - 1;
    }
    apply(low);
  }

  /**
   * Renders the parsed reply and trims it in one place, so the trim always runs
   * against markup that is already in the document and measurable. The card is
   * created by the hover that reveals it, and on that first pass the node has no
   * box yet — trimming against a zero height would drop every word — so the
   * measurement waits for a frame in which the element actually has one.
   */
  function peekBody(node: HTMLElement, html: string) {
    let frame = 0;
    const clampWhenMeasurable = (): void => {
      if (node.clientHeight) clampToBox(node);
      else frame = requestAnimationFrame(clampWhenMeasurable);
    };
    const render = (next: string): void => {
      cancelAnimationFrame(frame);
      node.innerHTML = next;
      clampWhenMeasurable();
    };
    render(html);
    return {update: render, destroy: () => cancelAnimationFrame(frame)};
  }

  /**
   * Nudges the peek back inside the window. It is centred on its point, and the
   * rail now reaches the full height of the column, so points near either end
   * would otherwise hang the card off the top or bottom edge.
   */
  function hover(index: number, event: Event): void {
    hoveredIndex = index;
    const middle = (event.currentTarget as HTMLElement).getBoundingClientRect();
    const centre = middle.top + middle.height / 2;
    const top = Math.max(0, PEEK_HALF_HEIGHT + 12 - centre);
    const bottom = Math.min(0, window.innerHeight - 12 - PEEK_HALF_HEIGHT - centre);
    peekShift = top || bottom;
  }

  function updateActive(): void {
    frame = 0;
    const targetY = window.innerHeight * 0.36;
    let closest: {id: string; distance: number} | null = null;
    for (const item of items) {
      const anchor = document.getElementById(`message-${item.id}`);
      if (!anchor) continue;
      const distance = Math.abs(anchor.getBoundingClientRect().top - targetY);
      if (!closest || distance < closest.distance) closest = {id: item.id, distance};
    }
    if (closest) activeId = closest.id;
  }

  function scheduleUpdate(): void {
    if (!frame) frame = requestAnimationFrame(updateActive);
  }

  function jumpTo(item: TimelineItem): void {
    activeId = item.id;
    document.getElementById(`message-${item.id}`)?.scrollIntoView({behavior: 'smooth', block: 'center'});
  }

  function edgeOpacity(index: number, count: number): number {
    const distance = Math.min(index, count - 1 - index);
    return [0.24, 0.42, 0.64, 0.82][distance] ?? 1;
  }

  /**
   * A bell curve around the pointer: the point under it is widest and fully
   * opaque, its neighbours taper off, and the ends of a long rail stay faint so
   * the whole thing reads as one shape rather than a list of ticks.
   */
  function buildProfiles(nextItems: TimelineItem[], hover: number | null, currentId: string) {
    return nextItems.map((item, index) => {
      const distance = hover === null ? Number.POSITIVE_INFINITY : Math.abs(hover - index);
      const edge = edgeOpacity(index, nextItems.length);
      const restingCurrent = hover === null && item.id === currentId;
      const opacity = restingCurrent
        ? Math.max(edge, 0.78)
        : distance === 0 ? 1 : distance === 1 ? Math.max(edge, 0.82) : distance === 2 ? Math.max(edge, 0.68) : edge;
      return {
        width: hover === null ? item.id === currentId ? 14 : 6 : HOVER_WIDTHS[distance] ?? 6,
        opacity,
        near: distance <= 2,
      };
    });
  }

  onMount(() => {
    // Capture: the conversation column scrolls, and scroll events do not bubble.
    document.addEventListener('scroll', scheduleUpdate, {capture: true, passive: true});
    window.addEventListener('resize', scheduleUpdate);
    scheduleUpdate();
  });

  afterUpdate(scheduleUpdate);

  onDestroy(() => {
    document.removeEventListener('scroll', scheduleUpdate, true);
    window.removeEventListener('resize', scheduleUpdate);
    if (frame) cancelAnimationFrame(frame);
  });
</script>

{#if items.length}
  <nav class:interacting={hoveredIndex !== null} class="timeline-rail" aria-label={$t('timeline.title')} onmouseleave={() => hoveredIndex = null}>
    {#each items as item, index (item.id)}
      <button
        type="button"
        class:active={activeId === item.id}
        class:hovered={hoveredIndex === index}
        class:near={profiles[index]?.near}
        class="timeline-point"
        aria-label={$t('timeline.jumpTo', {text: item.prompt || item.reply})}
        aria-current={activeId === item.id ? 'location' : undefined}
        data-tooltip="none"
        onmouseenter={(event) => hover(index, event)}
        onfocus={(event) => hover(index, event)}
        onblur={() => hoveredIndex = null}
        onclick={() => jumpTo(item)}
      >
        <span class="timeline-line" style={`--timeline-width:${profiles[index]?.width ?? 6}px;--timeline-opacity:${profiles[index]?.opacity ?? 1}`}></span>
        {#if hoveredIndex === index}
          <span class="timeline-preview" aria-hidden="true" style={`--peek-shift:${peekShift}px`}>
            {#if item.prompt}<strong>{item.prompt}</strong>{/if}
            {#if peekHtml}<span class="timeline-preview-body markdown-body" use:peekBody={peekHtml}></span>{/if}
          </span>
        {/if}
      </button>
    {/each}
  </nav>
{/if}
