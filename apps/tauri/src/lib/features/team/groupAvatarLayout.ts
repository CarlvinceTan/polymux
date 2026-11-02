export type GroupAvatarLayout = {
  kind: 'single' | 'pair' | 'triple' | 'quad';
  visibleCount: number;
  tileScale: number;
};

/** Keep small groups recognisable; larger groups reserve the fourth slot for a count. */
export function groupAvatarLayout(memberCount: number): GroupAvatarLayout {
  const count = Math.max(0, Math.floor(memberCount));
  if (count <= 1) return {kind: 'single', visibleCount: count, tileScale: 1};
  if (count === 2) return {kind: 'pair', visibleCount: 2, tileScale: .72};
  if (count === 3) return {kind: 'triple', visibleCount: 3, tileScale: .62};
  if (count === 4) return {kind: 'quad', visibleCount: 4, tileScale: .58};
  return {kind: 'triple', visibleCount: 3, tileScale: .62};
}
