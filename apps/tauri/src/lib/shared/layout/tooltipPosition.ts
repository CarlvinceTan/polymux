export type TooltipAlignment = 'start' | 'center' | 'end';

export function tooltipLeft(
  target: {left: number; right: number; width: number},
  tooltipWidth: number,
  viewportWidth: number,
  alignment: TooltipAlignment = 'center',
  inset = 8,
): number {
  const centered = target.left + target.width / 2 - tooltipWidth / 2;
  const minimumLeft = inset;
  const maximumLeft = viewportWidth - tooltipWidth - inset;
  if (centered >= minimumLeft && centered <= maximumLeft) return centered;

  const ideal = alignment === 'start'
    ? target.left
    : alignment === 'end'
      ? target.right - tooltipWidth
      : centered;
  return Math.max(minimumLeft, Math.min(maximumLeft, ideal));
}

export function tooltipTop(
  target: {top: number; bottom: number},
  tooltipHeight: number,
  viewportHeight: number,
  gap = 6,
  inset = 8,
): number {
  const below = target.bottom + gap;
  return below + tooltipHeight <= viewportHeight - inset
    ? below
    : Math.max(inset, target.top - tooltipHeight - gap);
}
