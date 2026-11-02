<script module lang="ts">
  /**
   * The task marks: seven flares built the way the Polymux logo is, so a task
   * is recognisable by its glyph rather than only by its title.
   *
   * Every arm is a single straight bar with square ends — nothing bends, and
   * nothing is an outline. What separates one flare from the next is arm count,
   * how far the arms reach, and whether there is a second tier, which is what
   * keeps them apart at 15px where a difference of angle alone would not.
   * Drawn as strokes, so one path set serves every size.
   *
   * State is carried by motion rather than colour. A flare that has stopped
   * sits still; one still working throws sparks — every bar rides outward along
   * its own track and off the end while the next enters behind it, endlessly.
   * Nothing ever travels inward: there is no return stroke to see, because the
   * dash pattern is exactly one cycle long, so the loop closes on itself.
   *
   * The travel is symmetric: every arm of a flare shares one period and one
   * gap, so they all leave and re-enter in unison and the mark holds its
   * symmetry at every instant, not only at rest. The period is shared rather
   * than derived per arm on purpose — the path coordinates are rounded, so arms
   * that the mark's own rotation maps onto each other differ in length by a
   * thousandth, and a per-arm period would let them drift out of phase.
   */
  export type TaskShape = 'burst' | 'quad' | 'star' | 'corona' | 'pinwheel' | 'halo' | 'spark';

  const SHAPES: Record<TaskShape, string> = {
    burst: 'M12 9.4L12 5.6 M13.84 10.16L16.53 7.47 M14.6 12L18.4 12 M13.84 13.84L16.53 16.53 M12 14.6L12 18.4 M10.16 13.84L7.47 16.53 M9.4 12L5.6 12 M10.16 10.16L7.47 7.47 M14.33 6.36L15.33 3.96 M17.64 9.67L20.04 8.67 M17.64 14.33L20.04 15.33 M14.33 17.64L15.33 20.04 M9.67 17.64L8.67 20.04 M6.36 14.33L3.96 15.33 M6.36 9.67L3.96 8.67 M9.67 6.36L8.67 3.96',
    quad: 'M12 9L12 2.4 M15 12L21.6 12 M12 15L12 21.6 M9 12L2.4 12 M14.12 9.88L15.68 8.32 M14.12 14.12L15.68 15.68 M9.88 14.12L8.32 15.68 M9.88 9.88L8.32 8.32',
    star: 'M12 9.6L12 3.2 M14.08 10.8L19.62 7.6 M14.08 13.2L19.62 16.4 M12 14.4L12 20.8 M9.92 13.2L4.38 16.4 M9.92 10.8L4.38 7.6',
    corona: 'M12 6.4L12 3.1 M14.8 7.15L16.45 4.29 M16.85 9.2L19.71 7.55 M17.6 12L20.9 12 M16.85 14.8L19.71 16.45 M14.8 16.85L16.45 19.71 M12 17.6L12 20.9 M9.2 16.85L7.55 19.71 M7.15 14.8L4.29 16.45 M6.4 12L3.1 12 M7.15 9.2L4.29 7.55 M9.2 7.15L7.55 4.29',
    pinwheel: 'M14.2 9.8L14.2 3.6 M15.01 12.81L20.37 9.71 M12.81 15.01L18.17 18.11 M9.8 14.2L9.8 20.4 M8.99 11.19L3.63 14.29 M11.19 8.99L5.83 5.89',
    halo: 'M12 6L12 2.6 M16.24 7.76L18.65 5.35 M18 12L21.4 12 M16.24 16.24L18.65 18.65 M12 18L12 21.4 M7.76 16.24L5.35 18.65 M6 12L2.6 12 M7.76 7.76L5.35 5.35 M12.77 10.15L13.3 8.86 M13.85 11.23L15.14 10.7 M13.85 12.77L15.14 13.3 M12.77 13.85L13.3 15.14 M11.23 13.85L10.7 15.14 M10.15 12.77L8.86 13.3 M10.15 11.23L8.86 10.7 M11.23 10.15L10.7 8.86',
    spark: 'M13.7 10.3L18.58 5.42 M13.7 13.7L18.58 18.58 M10.3 13.7L5.42 18.58 M10.3 10.3L5.42 5.42 M12 5.6L12 3.4 M18.4 12L20.6 12 M12 18.4L12 20.6 M5.6 12L3.4 12',
  };

  const ORDER: TaskShape[] = ['burst', 'quad', 'star', 'corona', 'pinwheel', 'halo', 'spark'];

  /** One arm of a flare, with the direction it travels when the task is
   * working — read off the bar itself, so the geometry stays the single source
   * of truth and no direction table can drift out of step with it. */
  export type FlareArm = {
    /** The bar at rest, and the track it rides out along — the same line, run
     * on past the tip by the length of the gap behind it. At offset 0 the dash
     * sits exactly on the resting bar, so a settled mark and the first frame of
     * a working one are the same geometry. */
    d: string;
    track: string;
    /** Dash and gap. They sum to `end`, the offset one full cycle costs, which
     * is what makes the travel loop seamlessly instead of jumping. */
    bar: number;
    gap: number;
    end: number;
    duration: number;
  };

  /**
   * Clear space behind each bar, in the glyph's own units.
   *
   * Wide on purpose: it is what keeps a bar whole as it travels, so each arm
   * reads as one moving piece rather than as a leading fragment and a trailing
   * one. The cost is that the blanks reach every arm's inner end at the same
   * moment — the arms move in unison — so for part of the cycle they line up
   * into a ring at the centre. Narrowing this closes the ring but beads the
   * bars; 4 is the value that was chosen looking at both.
   */
  const GAP = 4;
  /** How fast the longest bar in a flare drifts, in units per second; it sets
   * the period every arm of that flare then shares. */
  const SPEED = 5.5;

  const ARM = /^M(-?[\d.]+) (-?[\d.]+)L(-?[\d.]+) (-?[\d.]+)$/;

  export function flareArms(paths: string): FlareArm[] {
    const bars = paths.split(' M').map((piece, index) => {
      const d = index === 0 ? piece : `M${piece}`;
      const match = ARM.exec(d);
      if (!match) return {d, track: d, bar: 1, gap: GAP};
      const [x1, y1, x2, y2] = match.slice(1).map(Number);
      const bar = Math.hypot(x2 - x1, y2 - y1) || 1;
      const dx = (x2 - x1) / bar;
      const dy = (y2 - y1) / bar;
      return {d, track: `M${x1} ${y1}L${(x2 + dx * GAP).toFixed(2)} ${(y2 + dy * GAP).toFixed(2)}`, bar, gap: GAP};
    });
    // A cycle is one bar plus one gap of travel. The longest arm sets the
    // period and the rest keep to it, so the whole flare restarts together.
    const duration = (Math.max(...bars.map((arm) => arm.bar)) + GAP) / SPEED;
    return bars.map((arm) => ({...arm, end: arm.bar + GAP, duration}));
  }

  /**
   * A task's shape, chosen from its id. Deterministic, so a task keeps the same
   * mark for as long as it exists — including after its transcript is reopened
   * from a stored run — without the shape having to be stored anywhere.
   */
  export function taskShape(id: string): TaskShape {
    let hash = 0;
    for (let at = 0; at < id.length; at += 1) hash = (hash * 31 + id.charCodeAt(at)) >>> 0;
    return ORDER[hash % ORDER.length];
  }
</script>

<script lang="ts">
  import {taskStatusTone, type TaskStatus} from '../../features/workspace/taskStatus';

  export let id = '';
  /** Overrides the id-derived shape, for previews and fixed rows. */
  export let shape: TaskShape | null = null;
  export let status: TaskStatus = 'active';
  export let size = 15;
  export let label = '';

  $: tone = taskStatusTone(status);
  $: flare = shape ?? taskShape(id);
  $: arms = flareArms(SHAPES[flare]);
  $: working = tone === 'running';
</script>

<svg
  class={`task-glyph ${tone}`}
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.9"
  stroke-linecap="butt"
  role={label ? 'img' : 'presentation'}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : 'true'}
  overflow="visible"
>{#each arms as arm, index (index)}<path
    d={working ? arm.track : arm.d}
    style={working
      ? `--bar:${arm.bar.toFixed(2)};--gap:${arm.gap.toFixed(2)};--end:-${arm.end.toFixed(2)}px;--dur:${arm.duration.toFixed(3)}s`
      : undefined}
  />{/each}</svg>
