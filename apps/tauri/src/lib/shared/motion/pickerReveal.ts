import {cubicInOut} from 'svelte/easing';

/** Reveals a fixed-size inner picker through a changing-height viewport.
 * Unlike `slide`, this never scales the top padding, so a picker opening
 * below the quick row cannot settle with a final search-field nudge.
 * Shared by the emoji and sticker pickers so the two cannot drift. */
export function pickerReveal(node: HTMLElement) {
  const height = Number.parseFloat(getComputedStyle(node).height);
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  return {
    duration: reducedMotion ? 0 : 320,
    easing: cubicInOut,
    css: (progress: number) => {
      const opacity = Math.min(progress * 4, 1);
      const edge = `rgba(0,0,0,${progress})`;
      return `height:${height * progress}px;min-height:0;overflow:clip;opacity:${opacity};` +
        `-webkit-mask-image:linear-gradient(to bottom,#000 calc(100% - 12px),${edge} 100%);` +
        `mask-image:linear-gradient(to bottom,#000 calc(100% - 12px),${edge} 100%)`;
    },
  };
}
