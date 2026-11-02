/** Shared geometry for the AvatarKit port. Units are the native 316-unit avatar frame. */
export const TAU = Math.PI * 2;
export const FRAME_SIZE = 316;
export const FACE_SIZE = 200;

export interface Point {
  x: number;
  y: number;
}

export const clamp = (value: number, low = 0, high = 1): number =>
  value < low ? low : value > high ? high : value;

export const lerp = (a: number, b: number, t: number): number => a + (b - a) * t;

/** The app rounds path coordinates to 1/100 of a unit, as the native renderer does. */
export const r2 = (value: number): number => Math.floor(value * 100 + 0.5) / 100;
