/**
 * Framework-free Team-avatar renderer adapted from Jérémy Perret's Bloub.
 *
 * Bloub source: https://github.com/jeremy-prt/bloub
 * Source revision: b4bb3c1b5f93c7b87a2e8d620f667c4093d97749
 * Copyright (c) 2026 Jérémy Perret, used under the MIT License.
 *
 * Polymux keeps Bloub's measured radial silhouettes, palette, spherical eye
 * projection, deterministic liveliness, mask-ready eye paths and idle eye-fit
 * corrections. The ids and comments are translated for Polymux's English API.
 * See THIRD_PARTY_NOTICES.md for the complete license notice.
 */
import type {TeamAvatarDto, TeamAvatarExpression, TeamAvatarShape} from '@polymux/protocol';

/** Renderer-only pose. Expressions never cross the Desktop/Host boundary. */
export type BloubRenderAvatar = TeamAvatarDto & {expression: TeamAvatarExpression};

export const BLOUB_PROFILE_SAMPLES = 64;
export const BLOUB_RADIUS = 100;
export const BLOUB_VIEWBOX_RADIUS = 118;

const TAU = Math.PI * 2;
const ANGLES = Array.from({length: BLOUB_PROFILE_SAMPLES}, (_, index) => index / BLOUB_PROFILE_SAMPLES * TAU);
const COS = ANGLES.map(Math.cos);
const SIN = ANGLES.map(Math.sin);
const clamp = (value: number, low = 0, high = 1) => value < low ? low : value > high ? high : value;
const lerp = (from: number, to: number, amount: number) => from + (to - from) * amount;
const r2 = (value: number) => Math.round(value * 100) / 100;

interface Point {x: number; y: number}

function profileFromPolygon(polygon: Point[], centerX: number, centerY: number): number[] {
  const radii = new Array<number>(BLOUB_PROFILE_SAMPLES).fill(0);
  for (let sample = 0; sample < BLOUB_PROFILE_SAMPLES; sample++) {
    const directionX = COS[sample] ?? 0;
    const directionY = SIN[sample] ?? 0;
    let best = 0;
    for (let index = 0; index < polygon.length; index++) {
      const start = polygon[index]!;
      const end = polygon[(index + 1) % polygon.length]!;
      const edgeX = end.x - start.x;
      const edgeY = end.y - start.y;
      const denominator = directionX * edgeY - directionY * edgeX;
      if (Math.abs(denominator) < 1e-9) continue;
      const relativeX = start.x - centerX;
      const relativeY = start.y - centerY;
      const distance = (relativeX * edgeY - relativeY * edgeX) / denominator;
      const position = (relativeX * directionY - relativeY * directionX) / denominator;
      if (distance > best && position >= 0 && position <= 1) best = distance;
    }
    radii[sample] = best;
  }
  return radii;
}

function hullOfCircles(
  x1: number,
  y1: number,
  radius1: number,
  x2: number,
  y2: number,
  radius2: number,
  steps = 96,
): Point[] {
  const deltaX = x2 - x1;
  const deltaY = y2 - y1;
  const distance = Math.hypot(deltaX, deltaY) || 1e-6;
  const base = Math.atan2(deltaY, deltaX);
  const spread = Math.acos(Math.max(-1, Math.min(1, (radius1 - radius2) / distance)));
  const points: Point[] = [];
  for (let index = 0; index <= steps / 2; index++) {
    const angle = base + spread + (TAU - 2 * spread) * index / (steps / 2);
    points.push({x: x1 + Math.cos(angle) * radius1, y: y1 + Math.sin(angle) * radius1});
  }
  for (let index = 0; index <= steps / 2; index++) {
    const angle = base - spread + 2 * spread * index / (steps / 2);
    points.push({x: x2 + Math.cos(angle) * radius2, y: y2 + Math.sin(angle) * radius2});
  }
  return points;
}

function normalize(radii: number[], maximum = 1): number[] {
  const peak = Math.max(...radii);
  if (peak <= 0) return radii;
  const scale = maximum / peak;
  return radii.map((radius) => radius * scale);
}

function superellipseProfile(power: number, scaleX = 1, scaleY = 1): number[] {
  return ANGLES.map((_, index) => {
    const cosine = Math.abs((COS[index] ?? 0) / scaleX) ** power;
    const sine = Math.abs((SIN[index] ?? 0) / scaleY) ** power;
    return (cosine + sine) ** (-1 / power);
  });
}

function unionOfCirclesProfile(circles: Array<{x: number; y: number; r: number}>): number[] {
  const radii = new Array<number>(BLOUB_PROFILE_SAMPLES).fill(0);
  for (let index = 0; index < BLOUB_PROFILE_SAMPLES; index++) {
    const directionX = COS[index] ?? 0;
    const directionY = SIN[index] ?? 0;
    let best = 0;
    for (const circle of circles) {
      const projection = directionX * circle.x + directionY * circle.y;
      const discriminant = projection * projection
        - (circle.x * circle.x + circle.y * circle.y - circle.r * circle.r);
      if (discriminant < 0) continue;
      best = Math.max(best, projection + Math.sqrt(discriminant));
    }
    radii[index] = best;
  }
  return radii;
}

function roundedPolygon(vertices: Point[], cornerRadius: number, arcSteps = 10): Point[] {
  const points: Point[] = [];
  const normal = (start: Point, end: Point) => {
    const deltaX = end.x - start.x;
    const deltaY = end.y - start.y;
    const length = Math.hypot(deltaX, deltaY) || 1;
    return Math.atan2(-deltaX / length, deltaY / length);
  };
  for (let index = 0; index < vertices.length; index++) {
    const previous = vertices[(index - 1 + vertices.length) % vertices.length]!;
    const current = vertices[index]!;
    const next = vertices[(index + 1) % vertices.length]!;
    const startAngle = normal(previous, current);
    const endAngle = normal(current, next);
    let delta = endAngle - startAngle;
    while (delta > Math.PI) delta -= TAU;
    while (delta < -Math.PI) delta += TAU;
    for (let step = 0; step <= arcSteps; step++) {
      const angle = startAngle + delta * step / arcSteps;
      points.push({
        x: current.x + Math.cos(angle) * cornerRadius,
        y: current.y + Math.sin(angle) * cornerRadius,
      });
    }
  }
  return points;
}

function regularPolygonProfile(
  sides: number,
  radius: number,
  cornerRadius: number,
  rotationDegrees = 0,
): number[] {
  const rotation = rotationDegrees * Math.PI / 180;
  const vertices = Array.from({length: sides}, (_, index) => {
    const angle = rotation + index / sides * TAU;
    return {
      x: Math.cos(angle) * (radius - cornerRadius),
      y: Math.sin(angle) * (radius - cornerRadius),
    };
  });
  return profileFromPolygon(roundedPolygon(vertices, cornerRadius), 0, 0);
}

const pebble = normalize(
  ANGLES.map((angle) => 1 + .075 * Math.cos(2 * angle + .5) + .035 * Math.cos(3 * angle + 2.1)),
  1.02,
);
const cloud = normalize(unionOfCirclesProfile([
  {x: -.44, y: .2, r: .54},
  {x: .46, y: .2, r: .5},
  {x: .02, y: .3, r: .6},
  {x: -.24, y: -.3, r: .48},
  {x: .3, y: -.24, r: .44},
]), 1.02);
const droplet = normalize(
  profileFromPolygon(hullOfCircles(0, .28, .66, 0, -.96, .05), 0, 0),
  1.04,
);
const capsule = profileFromPolygon(hullOfCircles(-.42, 0, .62, .42, 0, .62), 0, 0);

export interface BloubShape {
  id: TeamAvatarShape;
  label: string;
  radii: number[];
}

export const BLOUB_SHAPES: BloubShape[] = [
  {id: 'circle', label: 'Circle', radii: new Array(BLOUB_PROFILE_SAMPLES).fill(1)},
  {id: 'pebble', label: 'Pebble', radii: pebble},
  {id: 'squircle', label: 'Squircle', radii: normalize(superellipseProfile(4.2), 1.15)},
  {id: 'capsule', label: 'Capsule', radii: capsule},
  {id: 'triangle', label: 'Triangle', radii: regularPolygonProfile(3, 1.12, .34, -90)},
  {id: 'hexagon', label: 'Hexagon', radii: regularPolygonProfile(6, 1.04, .26, 0)},
  // Corner-on cube silhouette: vertical sides and rounded top/bottom vertices.
  {id: 'cube', label: 'Cube', radii: regularPolygonProfile(6, 1.04, .18, -90)},
  {id: 'cloud', label: 'Cloud', radii: cloud},
  {id: 'droplet', label: 'Droplet', radii: droplet},
];
export const BLOUB_SHAPE_BY_ID = new Map<TeamAvatarShape, BloubShape>(
  BLOUB_SHAPES.map((shape) => [shape.id, shape]),
);

export interface BloubColor {id: string; label: string; hex: string}
export const BLOUB_COLORS: BloubColor[] = [
  {id: 'ink', label: 'Ink', hex: '#0a0a0c'},
  {id: 'brown', label: 'Brown', hex: '#8b5e3c'},
  {id: 'red', label: 'Red', hex: '#e8483f'},
  {id: 'orange', label: 'Orange', hex: '#f08a24'},
  {id: 'amber', label: 'Amber', hex: '#f0b429'},
  {id: 'green', label: 'Green', hex: '#3ecf8e'},
  {id: 'turquoise', label: 'Turquoise', hex: '#2fbfa0'},
  {id: 'blue', label: 'Blue', hex: '#3b93f0'},
  {id: 'violet', label: 'Violet', hex: '#8b5cf6'},
  {id: 'pink', label: 'Pink', hex: '#e152b0'},
  {id: 'grey', label: 'Grey', hex: '#a3a3a3'},
  {id: 'cream', label: 'Cream', hex: '#f1efe9'},
];

export interface HeadGaze {yaw: number; pitch: number; roll: number}
interface EyeConfig {w: number; h: number; tilt?: number; open: number}
export interface BloubExpression {
  id: TeamAvatarExpression;
  label: string;
  gaze: HeadGaze;
  split: number;
  eyes: [EyeConfig, EyeConfig];
}

const EYE_SPLIT = 15.46;
const EYE_WIDTH = .186;
const EYE_HEIGHT = .412;
const REST_GAZE: HeadGaze = {yaw: 28.49, pitch: 28.62, roll: -13};
const eye = (w: number, h: number, tilt = 0, open = 1): EyeConfig => ({w, h, tilt, open});
const pair = (w: number, h: number, tilt = 0, open = 1): [EyeConfig, EyeConfig] => [
  eye(w, h, tilt, open),
  eye(w, h, -tilt, open),
];

export const BLOUB_EXPRESSIONS: BloubExpression[] = [
  {id: 'neutral', label: 'Neutral', gaze: {...REST_GAZE}, split: EYE_SPLIT, eyes: [eye(EYE_WIDTH, EYE_HEIGHT), eye(EYE_WIDTH, EYE_HEIGHT)]},
  {id: 'attentive', label: 'Attentive', gaze: {yaw: 4, pitch: 5, roll: -4}, split: 16, eyes: pair(.21, .44)},
  {id: 'surprised', label: 'Surprised', gaze: {yaw: 3, pitch: -3, roll: 0}, split: 19, eyes: pair(.45, .47)},
  {id: 'excited', label: 'Excited', gaze: {yaw: 6, pitch: -14, roll: 0}, split: 19.5, eyes: pair(.4, .56, -10)},
  {id: 'happy', label: 'Happy', gaze: {yaw: 5, pitch: 9, roll: 0}, split: 17, eyes: pair(.27, .17, 14)},
  {id: 'laughing', label: 'Laughing', gaze: {yaw: 4, pitch: 14, roll: 0}, split: 18, eyes: pair(.34, .13, 20)},
  {id: 'angry', label: 'Angry', gaze: {yaw: 3, pitch: 7, roll: 0}, split: 17, eyes: pair(.34, .15, 30)},
  {id: 'sad', label: 'Sad', gaze: {yaw: 3, pitch: -13, roll: 0}, split: 16, eyes: pair(.22, .4, -28)},
  {id: 'frightened', label: 'Frightened', gaze: {yaw: 2, pitch: -20, roll: 0}, split: 20.5, eyes: pair(.4, .6)},
  {id: 'suspicious', label: 'Suspicious', gaze: {yaw: 12, pitch: 6, roll: -6}, split: 16, eyes: [eye(.21, .4), eye(.22, .15)]},
  {id: 'confused', label: 'Confused', gaze: {yaw: -14, pitch: 3, roll: 8}, split: 16.5, eyes: [eye(.2, .44, -18), eye(.28, .17, 14)]},
  {id: 'curious', label: 'Curious', gaze: {yaw: 16, pitch: -9, roll: -15}, split: 16.5, eyes: [eye(.24, .46, -8), eye(.2, .38, -8)]},
  {id: 'proud', label: 'Proud', gaze: {yaw: 5, pitch: 17, roll: 0}, split: 17, eyes: pair(.3, .15, 18)},
  {id: 'shy', label: 'Shy', gaze: {yaw: -19, pitch: -14, roll: -7}, split: 14, eyes: pair(.17, .3)},
  {id: 'unimpressed', label: 'Unimpressed', gaze: {yaw: -22, pitch: 2, roll: 0}, split: 16, eyes: pair(.3, .12)},
  {id: 'sleepy', label: 'Sleepy', gaze: {yaw: 6, pitch: -9, roll: -3}, split: 16, eyes: pair(.2, .42, 0, .42)},
];
export const BLOUB_EXPRESSION_BY_ID = new Map<TeamAvatarExpression, BloubExpression>(
  BLOUB_EXPRESSIONS.map((expression) => [expression.id, expression]),
);

type Vector3 = [number, number, number];
interface EyePose {x: number; y: number; a: number; b: number; c: number; d: number; depth: number}
const degrees = (value: number) => value * Math.PI / 180;

function spin(first: Vector3, second: Vector3, angle: number): [Vector3, Vector3] {
  const cosine = Math.cos(angle);
  const sine = Math.sin(angle);
  return [
    [first[0] * cosine + second[0] * sine, first[1] * cosine + second[1] * sine, first[2] * cosine + second[2] * sine],
    [second[0] * cosine - first[0] * sine, second[1] * cosine - first[1] * sine, second[2] * cosine - first[2] * sine],
  ];
}

export function bloubEyePoses(gaze: HeadGaze, scale: number, split = EYE_SPLIT): [EyePose, EyePose] {
  let forward: Vector3 = [0, 0, 1];
  let right: Vector3 = [1, 0, 0];
  let down: Vector3 = [0, 1, 0];
  [forward, right] = spin(forward, right, degrees(gaze.yaw));
  [down, forward] = spin(down, forward, degrees(gaze.pitch));
  [right, down] = spin(right, down, degrees(gaze.roll));
  const build = (side: number): EyePose => {
    const [eyeForward, eyeRight] = spin(forward, right, degrees(split * side));
    return {
      x: eyeForward[0] * scale,
      y: eyeForward[1] * scale,
      a: eyeRight[0],
      b: eyeRight[1],
      c: down[0],
      d: down[1],
      depth: eyeForward[2],
    };
  };
  return [build(-1), build(1)];
}

function createRng(seed: number): () => number {
  let value = seed >>> 0;
  return () => {
    value = value + 0x6d2b79f5 >>> 0;
    let mixed = Math.imul(value ^ value >>> 15, 1 | value);
    mixed = mixed + Math.imul(mixed ^ mixed >>> 7, 61 | mixed) ^ mixed;
    return ((mixed ^ mixed >>> 14) >>> 0) / 4294967296;
  };
}

function loopNoise(time: number, period: number, seed = 0): number {
  const phase = time / period * TAU;
  return .55 * Math.sin(phase + seed)
    + .3 * Math.sin(2 * phase + seed * 1.7 + 1.1)
    + .15 * Math.sin(3 * phase + seed * 2.3 + 2.4);
}

const blinkRng = createRng(0x5eed);
const blinks = (() => {
  const starts: number[] = [];
  let time = 1.4;
  while (time < 900) {
    starts.push(time);
    time += 1.9 + blinkRng() * 2.7;
    if (blinkRng() < .18) {
      starts.push(time);
      time += .24;
    }
  }
  return starts;
})();

function blinkLid(time: number): number {
  for (const start of blinks) {
    if (time < start) break;
    const progress = (time - start) / .18;
    if (progress >= 0 && progress <= 1)
      return progress < .45 ? 1 - progress / .45 : (progress - .45) / .55;
  }
  return 1;
}

function liveliness(time: number, animated: boolean) {
  if (!animated) return {dYaw: 0, dPitch: 0, dRoll: 0, lid: 1, driftX: 0, driftY: 0, breath: 1};
  return {
    dYaw: loopNoise(time, 11.3, .4) * 5.5 + loopNoise(time, 3.7, 2.1) * 1.6,
    dPitch: loopNoise(time, 9.1, 1.3) * 4.2 + loopNoise(time, 4.3, .7) * 1.3,
    dRoll: loopNoise(time, 13.7, 3.2) * 2.2,
    lid: blinkLid(time),
    driftX: loopNoise(time, 7.9, 1.9) * .006,
    driftY: loopNoise(time, 5.3, .3) * .007,
    breath: 1 + Math.sin(time / 3.4 * TAU) * .005,
  };
}

function blinkScale(lid: number): number {
  return .06 + .94 * clamp(lid);
}

function radiusAtAngle(radii: number[], angle: number): number {
  const normalized = (((angle / TAU % 1) + 1) % 1) * radii.length;
  const index = Math.floor(normalized);
  return lerp(radii[index % radii.length] ?? 1, radii[(index + 1) % radii.length] ?? 1, normalized - index);
}

function closedPath(points: Point[], tension = 1 / 6): string {
  if (points.length < 3) return '';
  let path = `M${r2(points[0]!.x)} ${r2(points[0]!.y)}`;
  for (let index = 0; index < points.length; index++) {
    const p0 = points[(index - 1 + points.length) % points.length]!;
    const p1 = points[index]!;
    const p2 = points[(index + 1) % points.length]!;
    const p3 = points[(index + 2) % points.length]!;
    const control1X = p1.x + (p2.x - p0.x) * tension;
    const control1Y = p1.y + (p2.y - p0.y) * tension;
    const control2X = p2.x - (p3.x - p1.x) * tension;
    const control2Y = p2.y - (p3.y - p1.y) * tension;
    path += `C${r2(control1X)} ${r2(control1Y)} ${r2(control2X)} ${r2(control2Y)} ${r2(p2.x)} ${r2(p2.y)}`;
  }
  return `${path}Z`;
}

function bodyPoints(radii: number[], centerX: number, centerY: number, scaleY: number): Point[] {
  return radii.map((radius, index) => ({
    x: ((COS[index] ?? 0) * radius + centerX) * BLOUB_RADIUS,
    y: ((SIN[index] ?? 0) * radius * scaleY + centerY) * BLOUB_RADIUS,
  }));
}

function capsulePath(width: number, height: number): string {
  const halfWidth = Math.max(width, .01) / 2;
  const halfHeight = Math.max(height, .01) / 2;
  const radius = Math.min(halfWidth, halfHeight);
  return `M${r2(-halfWidth)} ${r2(-halfHeight + radius)}`
    + `A${r2(radius)} ${r2(radius)} 0 0 1 ${r2(-halfWidth + radius)} ${r2(-halfHeight)}`
    + `L${r2(halfWidth - radius)} ${r2(-halfHeight)}`
    + `A${r2(radius)} ${r2(radius)} 0 0 1 ${r2(halfWidth)} ${r2(-halfHeight + radius)}`
    + `L${r2(halfWidth)} ${r2(halfHeight - radius)}`
    + `A${r2(radius)} ${r2(radius)} 0 0 1 ${r2(halfWidth - radius)} ${r2(halfHeight)}`
    + `L${r2(-halfWidth + radius)} ${r2(halfHeight)}`
    + `A${r2(radius)} ${r2(radius)} 0 0 1 ${r2(-halfWidth)} ${r2(halfHeight - radius)}Z`;
}

/** Idle corrections generated by Bloub's tested pose-space eye-fit solver. */
const IDLE_FACE_OFFSETS: Partial<Record<TeamAvatarShape, Partial<Record<TeamAvatarExpression, Point>>>> = {
  pebble: {
    neutral: {x: -.011836, y: .020501}, excited: {x: 0, y: -.070507}, sad: {x: 0, y: -.21489},
    frightened: {x: 0, y: -.064218}, confused: {x: .260393, y: 0}, curious: {x: -.035521, y: -.020508},
    shy: {x: .014352, y: -.024859}, sleepy: {x: 0, y: -.21},
  },
  squircle: {
    suspicious: {x: -.016576, y: -.00957}, shy: {x: .005488, y: -.009505}, unimpressed: {x: .017819, y: 0},
  },
  capsule: {
    neutral: {x: -.044766, y: .077537}, excited: {x: -.105, y: -.181865}, happy: {x: -.105, y: .181865},
    laughing: {x: -.033496, y: .058017}, sad: {x: -.105, y: -.181865}, frightened: {x: 0, y: -.249108},
    suspicious: {x: -.090933, y: .0525}, confused: {x: .030963, y: 0}, curious: {x: -.058017, y: -.033496},
    proud: {x: 0, y: .080664}, shy: {x: .113419, y: -.065483}, sleepy: {x: -.061569, y: -.035547},
  },
  triangle: {
    neutral: {x: -.003002, y: .0052}, laughing: {x: -.010254, y: .01776}, suspicious: {x: -.105, y: .181865},
    confused: {x: .056172, y: .032431}, curious: {x: -.21, y: 0}, proud: {x: -.027344, y: .047361},
  },
  hexagon: {
    laughing: {x: 0, y: .047852}, suspicious: {x: -.015039, y: .026048}, confused: {x: .268356, y: 0},
    curious: {x: -.031969, y: -.018457}, proud: {x: -.01709, y: .0296}, shy: {x: .016915, y: -.009766},
    unimpressed: {x: .009308, y: 0},
  },
  cloud: {
    neutral: {x: -.017075, y: .029574}, suspicious: {x: -.105, y: .181865}, confused: {x: .045727, y: .026401},
    curious: {x: -.080664, y: 0}, shy: {x: .325652, y: -.188015}, unimpressed: {x: .031067, y: .017936},
  },
  droplet: {
    neutral: {x: -.017095, y: .029609}, laughing: {x: -.008203, y: .014208}, suspicious: {x: -.023926, y: .041441},
    confused: {x: .046177, y: .02666}, proud: {x: -.020508, y: .035521},
  },
};

/** Static silhouette centre, rather than the radial origin used for eye projection. */
export function bloubVerticalCenter(shapeId: TeamAvatarShape): number {
  const shape = BLOUB_SHAPE_BY_ID.get(shapeId) ?? BLOUB_SHAPES[0]!;
  const ys = bodyPoints(shape.radii, 0, 0, 1).map(({y}) => y);
  return (Math.min(...ys) + Math.max(...ys)) / 2;
}

export interface BloubRenderedEye {path: string; matrix: string; opacity: number}
export interface BloubFrame {bodyPath: string; eyes: BloubRenderedEye[]}

export function renderBloubFrame(avatar: BloubRenderAvatar, time = 1, animated = true): BloubFrame {
  const shape = BLOUB_SHAPE_BY_ID.get(avatar.shape) ?? BLOUB_SHAPES[0]!;
  const expression = BLOUB_EXPRESSION_BY_ID.get(avatar.expression) ?? BLOUB_EXPRESSIONS[0]!;
  const life = liveliness(time, animated);
  const gaze = {
    yaw: expression.gaze.yaw + life.dYaw,
    pitch: expression.gaze.pitch + life.dPitch,
    roll: expression.gaze.roll + life.dRoll,
  };
  const bodyPath = closedPath(bodyPoints(shape.radii, life.driftX, life.driftY, life.breath));
  const offset = IDLE_FACE_OFFSETS[shape.id]?.[expression.id] ?? {x: 0, y: 0};
  const eyes: BloubRenderedEye[] = [];
  for (const [index, pose] of bloubEyePoses(gaze, BLOUB_RADIUS, expression.split).entries()) {
    if (pose.depth <= .02) continue;
    const config = expression.eyes[index]!;
    const localRadius = radiusAtAngle(shape.radii, Math.atan2(pose.y, pose.x));
    const tilt = degrees(config.tilt ?? 0);
    const cosine = Math.cos(tilt);
    const sine = Math.sin(tilt);
    const axisX = pose.a * cosine + pose.c * sine;
    const axisY = pose.b * cosine + pose.d * sine;
    const crossX = -pose.a * sine + pose.c * cosine;
    const crossY = -pose.b * sine + pose.d * cosine;
    const blink = blinkScale(Math.min(life.lid, config.open));
    eyes.push({
      path: capsulePath(config.w * BLOUB_RADIUS, config.h * BLOUB_RADIUS),
      matrix: `matrix(${r2(axisX)},${r2(axisY * blink)},${r2(crossX)},${r2(crossY * blink)},${r2(pose.x * localRadius + (life.driftX + offset.x) * BLOUB_RADIUS)},${r2(pose.y * localRadius + (life.driftY + offset.y) * BLOUB_RADIUS)})`,
      opacity: clamp(pose.depth / .12),
    });
  }
  return {bodyPath, eyes};
}
