import { TAU, type Point } from './math';
import { NEUTRAL_EYE, closedEye, circlePoints, closedCurve } from './eye';

/** A 3x3 rotation, applied to points with x right, y down and z towards the viewer. */
export class Rotation {
  readonly m: number[];

  constructor(m: number[]) {
    this.m = m;
  }

  static identity(): Rotation {
    return new Rotation([1, 0, 0, 0, 1, 0, 0, 0, 1]);
  }

  static turn(turn: [number, number, number]): Rotation {
    return Rotation.fromAngles(turn[1], turn[0], -turn[2]);
  }

  static fromAngles(yaw: number, pitch: number, roll: number): Rotation {
    const rad = Math.PI / 180;
    const cy = Math.cos(yaw * rad);
    const sy = Math.sin(yaw * rad);
    const cp = Math.cos(pitch * rad);
    const sp = Math.sin(pitch * rad);
    const cr = Math.cos(roll * rad);
    const sr = Math.sin(roll * rad);
    const turnYaw = new Rotation([cy, 0, sy, 0, 1, 0, -sy, 0, cy]);
    const turnPitch = new Rotation([1, 0, 0, 0, cp, sp, 0, -sp, cp]);
    const turnRoll = new Rotation([cr, -sr, 0, sr, cr, 0, 0, 0, 1]);
    return turnRoll.multiply(turnYaw).multiply(turnPitch);
  }

  multiply(other: Rotation): Rotation {
    const out = new Array<number>(9).fill(0);
    for (let r = 0; r < 3; r++) {
      for (let c = 0; c < 3; c++) {
        for (let k = 0; k < 3; k++) out[r * 3 + c] += this.m[r * 3 + k] * other.m[k * 3 + c];
      }
    }
    return new Rotation(out);
  }

  apply(point: FramePoint): FramePoint {
    return {
      x: this.m[0] * point.x + this.m[1] * point.y + this.m[2] * point.z,
      y: this.m[3] * point.x + this.m[4] * point.y + this.m[5] * point.z,
      z: this.m[6] * point.x + this.m[7] * point.y + this.m[8] * point.z,
    };
  }
}

export interface FramePoint {
  x: number;
  y: number;
  z: number;
}

/** Grok Bot's resting turn, which carries the front-facing eyes up and to the right. */
export const REST_TURN: [number, number, number] = [-15.896271043294888, 33.29315635042974, 25.38480127460161];

/** Where the two eyes sit on the sphere facing front: middle, outward and down. */
const SPHERE_EYES: { m: FramePoint; c: FramePoint; t: FramePoint }[] = [
  {
    m: { x: -0.3153457572898143, y: -0.000413547232064401, z: 0.948976755425548 },
    c: { x: -0.948976051487804, y: 0.0014310514033447285, z: -0.31534489974391217 },
    t: { x: -0.0012276245071586862, y: -0.9999988905346685, z: -0.000843722407896131 },
  },
  {
    m: { x: 0.3153457572898147, y: 0.000413547232064142, z: 0.948976755425548 },
    c: { x: 0.9489760638921817, y: 0.0011460633475971695, z: -0.31534602692677954 },
    t: { x: 0.0012179979536934692, y: -0.9999992577584695, z: 0.000031040210269326975 },
  },
];

/** Clip eyes wrapped round the sphere, each outline point carried its arc length to the surface. */
function sphereEyes(outline: Point[], body: Rotation, size: number): Point[][] {
  if (outline.length < 3) return [];
  return SPHERE_EYES.map((frame) => {
    const m = body.apply(frame.m);
    const c = body.apply(frame.c);
    const t = body.apply(frame.t);
    const surface = outline.map((point) => {
      const u = point.x;
      const v = point.y;
      const arc = Math.hypot(u, v);
      const k = arc > 1e-12 ? Math.sin(arc) / arc : 1;
      const h = Math.cos(arc);
      return {
        x: h * m.x + k * (u * c.x + v * t.x),
        y: h * m.y + k * (u * c.y + v * t.y),
        z: h * m.z + k * (u * c.z + v * t.z),
      };
    });
    const near = nearSide(surface, Math.atan2(m.y, m.x));
    return near?.map((point) => ({ x: point.x * size, y: point.y * size }));
  }).filter((points): points is Point[] => points !== undefined);
}

/** The near half of a loop on the unit sphere, with hidden runs folded along the rim. */
function nearSide(points: FramePoint[], facing: number): Point[] | undefined {
  const count = points.length;
  let start = -1;
  for (let index = 0; index < count; index++) {
    if (points[index].z <= 0 && points[(index + 1) % count].z > 0) {
      start = index;
      break;
    }
  }
  if (start < 0) {
    if (count === 0 || points[0].z <= 0) return undefined;
    return points.map((point) => ({ x: point.x, y: point.y }));
  }
  const crossing = (a: FramePoint, b: FramePoint): Point => {
    const k = a.z / (a.z - b.z);
    const x = a.x + (b.x - a.x) * k;
    const y = a.y + (b.y - a.y) * k;
    const length = Math.max(1e-12, Math.hypot(x, y));
    return { x: x / length, y: y / length };
  };
  const wrap = (angle: number): number => angle - TAU * Math.round(angle / TAU);
  const rim = (from: Point, to: Point): Point[] => {
    const begin = Math.atan2(from.y, from.x);
    const sweep = wrap(Math.atan2(to.y, to.x) - facing) - wrap(begin - facing);
    const steps = Math.max(1, Math.round(Math.ceil(Math.abs(sweep) / (Math.PI / 48))));
    const out: Point[] = [];
    for (let step = 1; step <= steps; step++) {
      if (step === steps) out.push(to);
      else {
        const angle = begin + (sweep * step) / steps;
        out.push({ x: Math.cos(angle), y: Math.sin(angle) });
      }
    }
    return out;
  };
  const out: Point[] = [];
  let entry: Point = { x: 1, y: 0 };
  let exit: Point | undefined;
  for (let step = 0; step < count; step++) {
    const a = points[(start + step) % count];
    const b = points[(start + step + 1) % count];
    if (a.z > 0 && b.z > 0) out.push({ x: b.x, y: b.y });
    else if (a.z > 0) {
      const point = crossing(a, b);
      exit = point;
      out.push(point);
    } else if (b.z > 0) {
      const point = crossing(a, b);
      if (exit) out.push(...rim(exit, point));
      else entry = point;
      out.push({ x: b.x, y: b.y });
    }
  }
  if (exit) out.push(...rim(exit, entry));
  return out;
}

export interface AvatarFrame {
  body: Point[];
  eyes: Point[][];
}

/**
 * The resting frame of a default (round, ink) avatar: a 100-unit body with the mood's
 * neutral face wrapped around it, turned by Grok Bot's rest turn.
 */
export function restingFrame(scale = 100): AvatarFrame {
  const body = closedCurve(circlePoints(scale));
  const eyes = sphereEyes(NEUTRAL_EYE, Rotation.turn(REST_TURN), scale);
  return { body, eyes: eyes.map((points) => closedCurve(points)) };
}

/** Eye outlines for an expression, closed to a slit when the avatar blinks. */
export function expressionEyes(closed: number): Point[] {
  return closed > 0 ? closedEye(NEUTRAL_EYE, closed) : NEUTRAL_EYE;
}

export { sphereEyes };
