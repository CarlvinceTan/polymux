import type {TeamAvatarDto} from '@polymux/protocol';
import {BLOUB_COLORS} from './model';

export type BloubTheme = 'light' | 'dark';

export const BLOUB_ADAPTIVE_MONOCHROME = {
  light: BLOUB_COLORS[0]!.hex,
  dark: BLOUB_COLORS.at(-1)!.hex,
} as const;

const sameColor = (left: string, right: string): boolean =>
  left.toLowerCase() === right.toLowerCase();

export function isAdaptiveMonochromeAvatar(avatar: TeamAvatarDto): boolean {
  const pair = avatar.colorPair;
  return Boolean(pair &&
    sameColor(pair.light, BLOUB_ADAPTIVE_MONOCHROME.light) &&
    sameColor(pair.dark, BLOUB_ADAPTIVE_MONOCHROME.dark));
}

export function adaptiveMonochromeAvatar(avatar: Pick<TeamAvatarDto, 'shape'>): TeamAvatarDto {
  return {
    shape: avatar.shape,
    color: BLOUB_ADAPTIVE_MONOCHROME.light,
    colorPair: {...BLOUB_ADAPTIVE_MONOCHROME},
  };
}

export function normalizeBloubAvatar(avatar: TeamAvatarDto): TeamAvatarDto {
  return isAdaptiveMonochromeAvatar(avatar) ? adaptiveMonochromeAvatar(avatar) : avatar;
}

export function bloubColorForTheme(avatar: TeamAvatarDto, theme: BloubTheme): string {
  return avatar.colorPair?.[theme] ?? avatar.color;
}
