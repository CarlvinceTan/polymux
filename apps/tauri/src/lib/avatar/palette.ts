/** Avatar palette, ported from the reference skin palette. */
export const AVATAR_COLORS: Record<string, string> = {
  encre: '#000000',
  brun: '#936439',
  rouge: '#FF263C',
  orange: '#FF6700',
  ambre: '#FF9800',
  vert: '#00C972',
  turquoise: '#00BCA6',
  bleu: '#1084FE',
  violet: '#9159FE',
  rose: '#FF309B',
  gris: '#777777',
  creme: '#f1efe9',
};

export interface AvatarDesignLite {
  shape: string;
  color: string;
  expression: string;
  followCursor: boolean;
  animates: boolean;
  playSequence: boolean;
}

export function avatarColor(design: AvatarDesignLite | undefined, dark: boolean): string {
  if (!design || design.color === 'encre' || design.color === 'creme') {
    return dark ? '#ffffff' : '#000000';
  }
  return AVATAR_COLORS[design.color] ?? (dark ? '#ffffff' : '#000000');
}

/** Eyes are the opposite of the body, adapting for the ink body like AvatarKit. */
export function eyeColor(design: AvatarDesignLite | undefined, dark: boolean): string {
  if (!design || design.color === 'encre' || design.color === 'creme') {
    return dark ? '#000000' : '#ffffff';
  }
  const hex = AVATAR_COLORS[design.color] ?? '#000';
  const value = parseInt(hex.replace('#', ''), 16);
  const rgb = [(value >> 16) & 255, (value >> 8) & 255, value & 255].map((v) => v / 255);
  const linear = rgb.map((v) => (v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)));
  const luminance = linear.reduce((sum, v, i) => sum + v * [0.2126, 0.7152, 0.0722][i], 0);
  return luminance > 0.179 ? '#000000' : '#ffffff';
}
