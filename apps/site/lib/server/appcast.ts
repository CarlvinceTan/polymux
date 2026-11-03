import type {ReleaseSummary} from '../content/types.ts';
import {githubClient} from './github.ts';
import {sendText, type Request, type Response} from './http.ts';

type Asset = {name: string; browser_download_url: string};
type RemoteRelease = {tag_name: string; draft?: boolean; prerelease?: boolean; assets: Asset[]};

function isRelease(value: unknown): value is RemoteRelease {
  if (!value || typeof value !== 'object') return false;
  const release = value as Partial<RemoteRelease>;
  return typeof release.tag_name === 'string' && Array.isArray(release.assets) &&
    release.assets.every((asset) => typeof asset?.name === 'string' &&
      typeof asset.browser_download_url === 'string' && asset.browser_download_url.startsWith('https://'));
}

async function readFeed(url: string, download: typeof fetch, timeoutMs: number) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const result = await download(url, {
      signal: controller.signal,
      headers: {'User-Agent': 'Polymux-Updater'},
    });
    return result.ok ? await result.text() : null;
  } catch {
    return null;
  } finally {
    clearTimeout(timeout);
  }
}

/** Serves the signed Sparkle feed attached to the newest published native release. */
export function createAppcast(
  releases: ReleaseSummary[],
  options: Parameters<typeof githubClient>[0] & {download?: typeof fetch} = {},
) {
  const {download = globalThis.fetch, ...githubOptions} = options;
  const github = githubClient(githubOptions);
  return async (_request: Request, response: Response) => {
    const entry = releases.find((release) =>
      release.downloadable && !release.historical && !release.version.includes('-'));
    if (!entry) {
      response.setHeader('Cache-Control', 'no-store');
      return sendText(response, 503, 'text/plain; charset=utf-8', 'Native updates are not published yet.');
    }
    const release = await github(`releases/tags/${encodeURIComponent(entry.tag)}`);
    if (!isRelease(release) || release.tag_name !== entry.tag || release.draft || release.prerelease) {
      response.setHeader('Cache-Control', 'no-store');
      return sendText(response, 503, 'text/plain; charset=utf-8', 'The latest Polymux update is temporarily unavailable.');
    }
    const asset = release.assets.find((item) => item.name === 'appcast.xml');
    if (!asset) {
      response.setHeader('Cache-Control', 'no-store');
      return sendText(response, 503, 'text/plain; charset=utf-8', 'The latest Polymux release has no update feed.');
    }
    const feed = await readFeed(asset.browser_download_url, download, githubOptions.timeoutMs ?? 10_000);
    if (!feed || !feed.includes('<rss')) {
      response.setHeader('Cache-Control', 'no-store');
      return sendText(response, 502, 'text/plain; charset=utf-8', 'The Polymux update feed could not be downloaded.');
    }
    response.setHeader('Cache-Control', 'public, s-maxage=300, stale-while-revalidate=60');
    return sendText(response, 200, 'application/xml; charset=utf-8', feed.endsWith('\n') ? feed : `${feed}\n`);
  };
}
