import { clamp } from './math';

/** AvatarKit's thinking dot, in units of the 100-point body radius. */
export function thinkingDot(index: number, time: number, amount: number, reduced: boolean) {
  const phase = (time / 1.4 + .119) % 1;
  const distance = Math.abs(phase - index / 3);
  const wave = reduced ? 1 : Math.exp(-(Math.min(distance, 1 - distance) ** 2) / .045);
  return {
    x: (index - 1) * 62 / 114.2705,
    y: reduced ? 0 : -wave * 9 / 114.2705 * amount,
    radius: 22 / 114.2705 * (reduced ? 1 : .84 + .22 * wave),
    opacity: reduced ? 1 : .5 + .5 * wave,
  };
}

const spring = (time: number) => time <= 0 ? 0 : 1 - (1 + time * 14) * Math.exp(-time * 14);

/** The visible round body and dots as ThinkingMorph enters and leaves. */
export class ThinkingMotion {
  amount = 0;
  time = 0;
  private active = false;
  private elapsed = 0;
  private from = 0;

  step(active: boolean, delta: number, reduced: boolean): boolean {
    if (active !== this.active) {
      this.active = active;
      this.elapsed = 0;
      this.from = this.amount;
      if (active && this.amount < .0001) this.time = 0;
    }
    this.elapsed += Math.max(0, Math.min(.064, delta));
    if (active || this.amount > .0001) this.time += Math.max(0, Math.min(.064, delta));
    this.amount = reduced ? Number(active) : active
      ? this.from + (1 - this.from) * spring(this.elapsed)
      : this.from * (1 - spring(this.elapsed));
    if (!active && this.amount < .0001) this.amount = 0;
    return active || this.amount > 0;
  }
}

export function thinkingZoom(size: number, amount: number): number {
  const compact = clamp((size - 44) / 90);
  return 1 + .5 * amount * (1 - compact * compact * (3 - 2 * compact));
}
