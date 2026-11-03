import {githubClient} from './github.ts';
import {sendJson, type Request, type Response} from './http.ts';

type Asset = {name: string; browser_download_url: string; size: number};
type Remote = {tag_name: string; draft?: boolean; html_url: string; assets: Asset[]};

const tag = 'dev-updates';
const payload = /^Polymux-dev-(\d+)\.(\d+)\.(\d+)\.zip$/;

function isRelease(value: unknown): value is Remote {
  if (!value || typeof value !== 'object') return false;
  const release = value as Partial<Remote>;
  return release.tag_name === tag && typeof release.html_url === 'string' && release.html_url.startsWith('https://') && Array.isArray(release.assets) &&
    release.assets.every((asset) => typeof asset?.name === 'string' && typeof asset.browser_download_url === 'string' &&
      asset.browser_download_url.startsWith('https://') && Number.isFinite(asset.size) && asset.size >= 0);
}

const parts = (name: string) => payload.exec(name)!.slice(1).map(Number);
const newer = (a: Asset, b: Asset) => {
  const [left, right] = [parts(a.name), parts(b.name)];
  const index = left.findIndex((part, i) => part !== right[i]);
  return index < 0 ? 0 : right[index] - left[index];
};

/** Serves the newest Nightly Mac build from the `dev-updates` prerelease. */
export function createNightly(options: Parameters<typeof githubClient>[0] = {}) {
  const github = githubClient(options);
  return async (_request: Request, response: Response) => {
    const release = await github(`releases/tags/${tag}`);
    const newest = isRelease(release) && !release.draft ? release.assets.filter((asset) => payload.test(asset.name)).sort(newer)[0] : null;
    if (!isRelease(release) || !newest) {
      response.setHeader('Cache-Control', 'public, s-maxage=30');
      return sendJson(response, 503, {error: 'No Nightly build is available yet.'});
    }
    response.setHeader('Cache-Control', 'public, s-maxage=300, stale-while-revalidate=60');
    return sendJson(response, 200, {
      version: parts(newest.name).join('.'),
      releaseUrl: release.html_url,
      macos: {name: newest.name, url: newest.browser_download_url, size: newest.size},
    });
  };
}
