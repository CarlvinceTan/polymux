export type SupportedPlatform = 'macos' | 'windows' | 'linux';

export type DownloadAsset = {name: string; url: string; size: number};
export type ReleaseDownloads = {
  version: string | null;
  releaseUrl: string;
  platforms: Record<SupportedPlatform, DownloadAsset | null>;
};

export const PLATFORM_LABELS: Record<SupportedPlatform, string> = {
  macos: 'macOS',
  windows: 'Windows',
  linux: 'Linux',
};

export const SUPPORTED_PLATFORMS: SupportedPlatform[] = ['macos', 'windows', 'linux'];

/** Native releases are macOS-first; only show platforms the release actually ships. */
export function visiblePlatforms(downloads: ReleaseDownloads | null): SupportedPlatform[] {
  if (!downloads) return ['macos'];
  return SUPPORTED_PLATFORMS.filter((platform) => downloads.platforms[platform]);
}

export function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '';
  const megabytes = bytes / 1_000_000;
  return megabytes >= 100 ? `${Math.round(megabytes)} MB` : `${megabytes.toFixed(1)} MB`;
}
