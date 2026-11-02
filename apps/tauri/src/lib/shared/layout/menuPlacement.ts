/** Matches `--menu-edge` in style.css. The dropdown shell uses
 * `border: 1px solid var(--neutral-200)`, so the box has to sit this far from
 * a clipping edge or the menu crowds the window bounds. */
export const MENU_EDGE_MARGIN = 16;

export function clampToMenuEdge(
  start: number,
  size: number,
  viewport: number,
  edge = MENU_EDGE_MARGIN,
): number {
  return Math.max(edge, Math.min(start, viewport - edge - size));
}

/** A full-width inset menu keeps this much air inside its parent on both
 * sides so the shell border sits cleanly within the host bounds. */
export function fitInsetMenu(
  preferredWidth: number,
  parent: {left: number; width: number},
  edge = MENU_EDGE_MARGIN,
): {width: number; left: number} {
  const width = Math.min(preferredWidth, Math.max(0, parent.width - 2 * edge));
  return {width, left: parent.left + (parent.width - width) / 2};
}
