import shapes from './shapes.json';
import plus from './plus.json';
import { Rotation, REST_TURN } from './frame';
import { r2, TAU, type Point } from './math';

// AvatarKit's profile and projected-solid geometry, in its 100-unit face space.
type Shape = { radii: number[]; eyes: number[][][] };
type Solid = { kind: 'slab' | 'loft' | 'revolved' | 'pill'; depth?: number; bevel?: number };
const solids: Record<string, Solid> = {
  nuage: { kind: 'loft', depth: .72 }, heart: { kind: 'loft', depth: .62 },
  goutte: { kind: 'revolved' }, wedge: { kind: 'revolved' }, tablet: { kind: 'pill' },
  square: { kind: 'slab', depth: .36, bevel: .22 }, sparkle: { kind: 'slab', depth: .36, bevel: .22 },
  clover: { kind: 'slab', depth: .36, bevel: .22 }, flower: { kind: 'slab', depth: .36, bevel: .22 },
  house: { kind: 'slab', depth: .36, bevel: .22 }, hexagone: { kind: 'slab', depth: .36, bevel: .22 },
  star: { kind: 'slab', depth: .3, bevel: .18 },
};

const rotation = Rotation.turn(REST_TURN);
const cache = new Map<string, { body: Path2D; eyes: Path2D }>();

function project(x: number, y: number, z: number): Point {
  const p = rotation.apply({ x, y, z });
  return { x: p.x, y: p.y };
}

function section(points: Point[], y: number): number {
  let radius = 0;
  for (let i = 0; i < points.length; i++) {
    const a = points[i], b = points[(i + 1) % points.length];
    if ((a.y <= y) === (b.y <= y)) continue;
    radius = Math.max(radius, Math.abs(a.x + (b.x - a.x) * (y - a.y) / (b.y - a.y)));
  }
  return radius;
}

function rings(radii: number[], solid: Solid): Point[][] {
  const base = radii.map((r, i) => ({ x: r * Math.cos(i / radii.length * TAU), y: r * Math.sin(i / radii.length * TAU) }));
  if (solid.kind === 'revolved' || solid.kind === 'pill') {
    const values = base.map((p) => solid.kind === 'revolved' ? p.y : p.x);
    const start = Math.min(...values), end = Math.max(...values);
    const radius = Math.max(...base.map((p) => Math.abs(p.y)));
    const reach = Math.max(0, Math.max(...base.map((p) => Math.abs(p.x))) - radius);
    return Array.from({ length: 41 }, (_, row) => {
      const axis = solid.kind === 'revolved' ? start + 1e-6 + (end - start - 2e-6) * row / 40 : start + (end - start) * row / 40;
      const offset = axis - Math.min(reach, Math.max(-reach, axis));
      const r = solid.kind === 'revolved' ? section(base, axis) : Math.sqrt(Math.max(0, radius * radius - offset * offset));
      return Array.from({ length: 64 }, (_, i) => {
        const angle = i / 64 * TAU;
        return solid.kind === 'revolved' ? project(r * Math.cos(angle), axis, r * Math.sin(angle))
          : project(axis, r * Math.cos(angle), r * Math.sin(angle));
      });
    });
  }
  const layers: { k: number; z: number }[] = [];
  if (solid.kind === 'loft') {
    for (let i = 0; i <= 16; i++) {
      const phi = -Math.PI / 2 + i * Math.PI / 16;
      layers.push({ k: Math.pow(Math.max(0, Math.cos(phi)), .72), z: solid.depth! * Math.sin(phi) });
    }
  } else {
    const bevel = solid.bevel!, core = Math.max(0, solid.depth! - bevel);
    const xs = base.map((p) => p.x), ys = base.map((p) => p.y);
    const radius = Math.max(.01, Math.min((Math.max(...xs) - Math.min(...xs)) / 2, (Math.max(...ys) - Math.min(...ys)) / 2));
    const angles = [0, 22.5, 45, 67.5, 90].map((v) => v * Math.PI / 180);
    for (const [side, list] of [[-1, [...angles].reverse()], [1, angles]] as const) {
      for (const phi of list) layers.push({ k: Math.max(0, 1 - bevel / radius * (1 - Math.cos(phi))), z: side * (core + bevel * Math.sin(phi)) });
    }
  }
  return layers.map(({ k, z }) => base.map((p) => project(p.x * k, p.y * k, z)));
}

function addLoop(path: Path2D, points: Point[]): void {
  const area = points.reduce((sum, p, i) => { const next = points[(i + 1) % points.length]; return sum + p.x * next.y - p.y * next.x; }, 0);
  const loop = area < 0 ? [...points].reverse() : points;
  path.moveTo(loop[0].x * 100, loop[0].y * 100);
  for (const p of loop.slice(1)) path.lineTo(p.x * 100, p.y * 100);
  path.closePath();
}

function slabPath(mesh: Point[][]): Path2D {
  const path = new Path2D();
  addLoop(path, mesh[0]);
  addLoop(path, mesh[mesh.length - 1]);
  for (let row = 1; row < mesh.length; row++) {
    const a = mesh[row - 1], b = mesh[row];
    for (let i = 0; i < a.length; i++) {
      const next = (i + 1) % a.length;
      addLoop(path, [a[i], a[next], b[next], b[i]]);
    }
  }
  return path;
}

function outlinePath(mesh: Point[][]): Path2D {
  const count = 128;
  const cos = Array.from({ length: count }, (_, i) => Math.cos(i / count * TAU));
  const sin = Array.from({ length: count }, (_, i) => Math.sin(i / count * TAU));
  const best = Array<number>(count).fill(0);
  function cast(p: Point, q: Point) {
    const ra = Math.hypot(p.x, p.y), rb = Math.hypot(q.x, q.y);
    if (ra < 1e-6 && rb < 1e-6) return;
    const a = Math.atan2(p.y, p.x), b = Math.atan2(q.y, q.x);
    const start = ra < 1e-6 ? b : a;
    let span = (rb < 1e-6 ? a : b) - start;
    while (span > Math.PI) span -= TAU;
    while (span < -Math.PI) span += TAU;
    const first = Math.floor((start + Math.min(0, span)) / TAU * count) - 1;
    const last = Math.floor((start + Math.max(0, span)) / TAU * count) + 1;
    const ex = q.x - p.x, ey = q.y - p.y;
    for (let i = first; i <= last; i++) {
      const s = ((i % count) + count) % count, dx = cos[s], dy = sin[s];
      const den = dx * ey - dy * ex;
      if (Math.abs(den) < 1e-12) continue;
      const t = (p.x * ey - p.y * ex) / den, u = (p.x * dy - p.y * dx) / den;
      if (t > best[s] && u >= -1e-9 && u <= 1 + 1e-9) best[s] = t;
    }
  }
  for (let row = 0; row < mesh.length; row++) {
    const ring = mesh[row];
    for (let i = 0; i < ring.length; i++) {
      cast(ring[i], ring[(i + 1) % ring.length]);
      if (row) cast(mesh[row - 1][i], ring[i]);
    }
  }
  const points = best.map((r, i) => ({ x: cos[i] * r * 100, y: sin[i] * r * 100 }));
  const path = new Path2D();
  path.moveTo(r2(points[0].x), r2(points[0].y));
  for (let i = 0; i < count; i++) {
    const p0 = points[(i + count - 1) % count], p1 = points[i], p2 = points[(i + 1) % count], p3 = points[(i + 2) % count];
    path.bezierCurveTo(r2(p1.x + (p2.x - p0.x) / 6), r2(p1.y + (p2.y - p0.y) / 6), r2(p2.x - (p3.x - p1.x) / 6), r2(p2.y - (p3.y - p1.y) / 6), r2(p2.x), r2(p2.y));
  }
  path.closePath();
  return path;
}

export function commandsPath(loops: number[][][]): Path2D {
  const path = new Path2D();
  for (const loop of loops) for (const [type, ...points] of loop) {
    if (type === 0) path.moveTo(points[0], points[1]);
    else if (type === 1) path.lineTo(points[0], points[1]);
    else if (type === 2) path.quadraticCurveTo(points[0], points[1], points[2], points[3]);
    else if (type === 3) path.bezierCurveTo(points[0], points[1], points[2], points[3], points[4], points[5]);
    else if (type === 4) path.closePath();
  }
  return path;
}

/** Shape profiles and neutral eyes are exported from AvatarKit's still frame. */
export function shapePaths(id: string): { body: Path2D; eyes: Path2D } | undefined {
  if (id === 'plus') {
    if (!cache.has(id)) cache.set(id, { body: commandsPath([plus.plus.body]), eyes: commandsPath(plus.plus.eyes) });
    return cache.get(id);
  }
  const shape = (shapes as Record<string, Shape>)[id], solid = solids[id];
  if (!shape || !solid) return undefined;
  if (!cache.has(id)) {
    const mesh = rings(shape.radii, solid);
    cache.set(id, { body: solid.kind === 'slab' ? slabPath(mesh) : outlinePath(mesh), eyes: commandsPath(shape.eyes) });
  }
  return cache.get(id);
}
