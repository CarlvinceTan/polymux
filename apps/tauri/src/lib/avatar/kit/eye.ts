import { TAU, clamp, r2, type Point } from './math';

/** A path as points plus, for each, whether it starts a new subpath (Svelte-free canvas fallback). */
export interface Outline {
  points: Point[];
  closed: boolean;
}

let cosTable: number[] | undefined;
let sinTable: number[] | undefined;

function tables(): { cos: number[]; sin: number[] } {
  if (!cosTable || !sinTable) {
    cosTable = [];
    sinTable = [];
    for (let i = 0; i < 64; i++) {
      const angle = (i / 64) * TAU;
      cosTable.push(Math.cos(angle));
      sinTable.push(Math.sin(angle));
    }
  }
  return { cos: cosTable, sin: sinTable };
}

/** A circle of `radius` in engine units, sampled like AvatarKit's 64-point silhouettes. */
export function circlePoints(radius: number): Point[] {
  const { cos, sin } = tables();
  return cos.map((value, index) => ({
    x: r2(value * radius),
    y: r2(sin[index] * radius),
  }));
}

/** Closed Catmull-Rom path points, matching AvatarKit's `closedPath` (tension 1/6). */
export function closedCurve(points: Point[], tension = 1 / 6): Point[] {
  const count = points.length;
  if (count < 3) return points;
  const out: Point[] = [];
  for (let i = 0; i < count; i++) {
    const p0 = points[(i - 1 + count) % count];
    const p1 = points[i];
    const p2 = points[(i + 1) % count];
    const p3 = points[(i + 2) % count];
    const steps = 12;
    for (let step = 0; step < steps; step++) {
      const t = step / steps;
      const s = 1 - t;
      const c1x = p1.x + (p2.x - p0.x) * tension;
      const c1y = p1.y + (p2.y - p0.y) * tension;
      const c2x = p2.x - (p3.x - p1.x) * tension;
      const c2y = p2.y - (p3.y - p1.y) * tension;
      out.push({
        x: r2(s * s * s * p1.x + 3 * s * s * t * c1x + 3 * s * t * t * c2x + t * t * t * p2.x),
        y: r2(s * s * s * p1.y + 3 * s * s * t * c1y + 3 * s * t * t * c2y + t * t * t * p2.y),
      });
    }
  }
  return out;
}

/** Adds a closed path to a canvas context. */
export function tracePath(context: CanvasRenderingContext2D, points: Point[]): void {
  if (points.length < 3) return;
  context.beginPath();
  context.moveTo(points[0].x, points[0].y);
  for (let i = 1; i < points.length; i++) context.lineTo(points[i].x, points[i].y);
  context.closePath();
}

/** Offsets a straight spine by `radius` and caps both ends, as AvatarKit's `stroke` does. */
export function stroke(spine: Point[], radius: number): Point[] {
  const points = spine.filter(
    (point, index) => index === 0 || Math.hypot(point.x - spine[index - 1].x, point.y - spine[index - 1].y) > 1e-6,
  );
  if (points.length < 2 || radius <= 0) return [];
  const unit = (a: Point, b: Point): Point => {
    const length = Math.hypot(b.x - a.x, b.y - a.y);
    return length > 0 ? { x: (b.x - a.x) / length, y: (b.y - a.y) / length } : { x: 0, y: 0 };
  };
  const normals: Point[] = [];
  for (let index = 0; index < points.length; index++) {
    const along = unit(points[Math.max(0, index - 1)], points[Math.min(points.length - 1, index + 1)]);
    let normal = { x: -along.y, y: along.x };
    const last = normals[normals.length - 1];
    if (last && normal.x * last.x + normal.y * last.y < 0) normal = { x: -normal.x, y: -normal.y };
    normals.push(normal);
  }
  const head = unit(points[0], points[1]);
  const tail = unit(points[points.length - 2], points[points.length - 1]);
  const left = points.map((point, index) => ({
    x: point.x + normals[index].x * radius,
    y: point.y + normals[index].y * radius,
  }));
  const right = points.map((point, index) => ({
    x: point.x - normals[index].x * radius,
    y: point.y - normals[index].y * radius,
  }));
  const end = { x: points[points.length - 1].x + tail.x * radius, y: points[points.length - 1].y + tail.y * radius };
  const start = { x: points[0].x - head.x * radius, y: points[0].y - head.y * radius };

  const cap = (center: Point, from: Point, toward: Point, capRadius: number): Point[] => {
    const angle = Math.atan2(from.y - center.y, from.x - center.x);
    let delta = Math.atan2(toward.y - center.y, toward.x - center.x) - angle;
    while (delta > Math.PI) delta -= TAU;
    while (delta < -Math.PI) delta += TAU;
    const sign = delta >= 0 ? 1 : -1;
    const out: Point[] = [];
    for (let step = 1; step < 10; step++) {
      const phase = angle + (step / 10) * sign * Math.PI;
      out.push({ x: center.x + capRadius * Math.cos(phase), y: center.y + capRadius * Math.sin(phase) });
    }
    return out;
  };

  return [
    ...left,
    ...cap(points[points.length - 1], left[left.length - 1], end, radius),
    ...right.slice().reverse(),
    ...cap(points[0], right[0], start, radius),
  ];
}

/** A straight spine `width` wide and `length` long, before its tilt and offset. */
export function stadiumOutline(width: number, length: number, degrees = 0): Point[] {
  const reach = Math.max(0, (length - width) / 2);
  const angle = (degrees * Math.PI) / 180;
  const s = Math.sin(angle) * reach;
  const c = Math.cos(angle) * reach;
  const spine: Point[] = [];
  for (let step = 0; step <= 20; step++) {
    const t = step / 20;
    spine.push({ x: -s + 2 * s * t, y: -c + 2 * c * t });
  }
  return stroke(spine, width / 2);
}

/** The neutral eye: a stadium 0.26 wide and 0.57 long in the face's unit-sphere space. */
export const NEUTRAL_EYE = stadiumOutline(0.26, 0.57);

/** Squeezes an eye outline towards a slit, as `EyeShape.closed` does. */
export function closedEye(points: Point[], amount: number): Point[] {
  if (amount <= 0) return points;
  const box = extent(points);
  const width = Math.max(box.w, 1e-4);
  const height = Math.max(box.h, width);
  const target = height + (width * 0.42 - height) * clamp(amount);
  const halfW = width / 2;
  const halfH = Math.max(1e-4, target) / 2;
  const radius = Math.min(halfW, halfH);
  const straight = Math.max(0, halfH - radius);
  return ovalOutline(halfW, straight + radius, radius, box.cx, box.cy);
}

function extent(points: Point[]): { w: number; h: number; cx: number; cy: number } {
  let minX = Infinity;
  let maxX = -Infinity;
  let minY = Infinity;
  let maxY = -Infinity;
  for (const point of points) {
    minX = Math.min(minX, point.x);
    maxX = Math.max(maxX, point.x);
    minY = Math.min(minY, point.y);
    maxY = Math.max(maxY, point.y);
  }
  return { w: maxX - minX, h: maxY - minY, cx: (minX + maxX) / 2, cy: (minY + maxY) / 2 };
}

/** A stadium outline of half-width `halfW`, half-length `halfH`, at (cx, cy). */
function ovalOutline(halfW: number, halfH: number, radius: number, cx: number, cy: number): Point[] {
  const straight = Math.max(0, halfH - radius);
  const out: Point[] = [];
  const cap = (centerY: number, from: number, to: number): void => {
    for (let step = 0; step <= 12; step++) {
      const angle = from + ((to - from) * step) / 12;
      out.push({ x: cx + radius * Math.cos(angle), y: cy + centerY + radius * Math.sin(angle) });
    }
  };
  cap(-straight, Math.PI, 0);
  cap(straight, 0, Math.PI);
  return out;
}
