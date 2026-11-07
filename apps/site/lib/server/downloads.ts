import type {ReleaseSummary} from '../content/types.ts';
import {githubClient} from './github.ts';
import {sendJson, type Request, type Response} from './http.ts';

type Asset = {name: string; browser_download_url: string; size: number};
type RemoteRelease = {tag_name: string; draft?: boolean; prerelease?: boolean; html_url: string; published_at?: string; assets: Asset[]};

function isRelease(value: unknown): value is RemoteRelease {
  if (!value || typeof value !== 'object') return false;
  const release = value as Partial<RemoteRelease>;
  return typeof release.tag_name === 'string' && typeof release.html_url === 'string' && release.html_url.startsWith('https://') && Array.isArray(release.assets) &&
    release.assets.every((asset) => typeof asset?.name === 'string' && typeof asset.browser_download_url === 'string' &&
      asset.browser_download_url.startsWith('https://') && Number.isFinite(asset.size) && asset.size >= 0);
}

export function createDownloads(releases: ReleaseSummary[], options: Parameters<typeof githubClient>[0] = {}) {
  const github = githubClient(options);
  return async (request: Request, response: Response) => {
    const raw = request.query?.version;
    const input = Array.isArray(raw) ? raw[0] : raw;
    if (input !== undefined && (typeof input !== 'string' || !/^v?\d+\.\d+\.\d+(?:-[\w.-]+)?$/.test(input))) {
      return sendJson(response, 400, {error: 'Invalid release version.'});
    }
    const version = input?.replace(/^v/, '');
    const candidates = releases.filter((release) => release.downloadable && !release.historical);
    const entry = version ? candidates.find((release) => release.version === version) : candidates.find((release) => !release.version.includes('-'));
    if (!entry) {
      response.setHeader('Cache-Control', version ? 'public, s-maxage=300' : 'public, s-maxage=30');
      return sendJson(response, version ? 404 : 503, {error: version ? 'That Polymux release is not available for download.' : 'Native downloads are not published yet.'});
    }
    const release = await github(`releases/tags/${encodeURIComponent(entry.tag)}`);
    if (!isRelease(release) || release.tag_name !== entry.tag || release.draft || (!version && release.prerelease)) {
      response.setHeader('Cache-Control', 'public, s-maxage=30');
      return sendJson(response, 503, {error: 'The Polymux download is temporarily unavailable.'});
    }
    const asset = (pattern: RegExp) => {
      const item = release.assets.find((item) => pattern.test(item.name));
      return item ? {name: item.name, url: item.browser_download_url, size: item.size} : null;
    };
    const platforms = {macos: asset(/\.dmg$/i), windows: asset(/Setup\.exe$/i), linux: asset(/\.AppImage$/i)};
    if (!Object.values(platforms).some(Boolean)) {
      response.setHeader('Cache-Control', 'public, s-maxage=30');
      return sendJson(response, 503, {error: 'This release has no native installers.'});
    }
    response.setHeader('Cache-Control', 'public, s-maxage=300, stale-while-revalidate=60');
    return sendJson(response, 200, {version: entry.version, publishedAt: release.published_at ?? null, releaseUrl: release.html_url, platforms});
  };
}
