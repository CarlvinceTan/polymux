import {publishedReleaseVersions} from '../lib/published-release-versions.js';

const REPOSITORY = 'CarlvinceTan/polymux';
const PUBLISHED_RELEASE_VERSIONS = new Set(publishedReleaseVersions);
const GITHUB_TIMEOUT_MS = 10_000;

function asset(release, pattern) {
  const match = release.assets?.find((item) => pattern.test(item.name));
  return match ? {name: match.name, url: match.browser_download_url, size: match.size} : null;
}

function requestedVersion(request) {
  const raw = request?.query?.version;
  const value = Array.isArray(raw) ? raw[0] : raw;
  if (typeof value !== 'string') return null;
  const version = value.trim().replace(/^v/, '');
  return /^[\w.+-]{1,64}$/.test(version) ? version : null;
}

async function fetchRelease(path) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), GITHUB_TIMEOUT_MS);
  timeout.unref?.();
  try {
    const result = await fetch(`https://api.github.com/repos/${REPOSITORY}/${path}`, {
      headers: {
        Accept: 'application/vnd.github+json',
        'User-Agent': 'Polymux-Website',
        'X-GitHub-Api-Version': '2022-11-28',
      },
      signal: controller.signal,
    });
    if (!result.ok) return null;
    return await result.json();
  } catch {
    return null;
  } finally {
    clearTimeout(timeout);
  }
}

export default async function handler(request, response) {
  const version = requestedVersion(request);
  let release = null;

  if (version) {
    if (!PUBLISHED_RELEASE_VERSIONS.has(version)) {
      response.setHeader('Cache-Control', 'public, s-maxage=300, stale-while-revalidate=60');
      return response.status(404).json({error: 'That Polymux release is not published.'});
    }
    const tagged = await fetchRelease(`releases/tags/v${encodeURIComponent(version)}`);
    if (tagged && !tagged.draft) release = tagged;
  } else {
    const releases = await fetchRelease('releases?per_page=10');
    if (Array.isArray(releases)) {
      release = releases.find((item) => !item.draft && !/^app-v/.test(item.tag_name ?? '')) ?? null;
    }
  }

  if (!release) {
    response.setHeader('Cache-Control', 'public, s-maxage=30');
    return response.status(503).json({error: 'No public Polymux release is available yet.'});
  }

  const releaseUrl = release.html_url ?? `https://github.com/${REPOSITORY}/releases/latest`;
  response.setHeader('Cache-Control', 'public, s-maxage=300, stale-while-revalidate=60');
  return response.status(200).json({
    version: release.tag_name?.replace(/^(?:app-)?v/, '') ?? null,
    publishedAt: release.published_at ?? null,
    releaseUrl,
    platforms: {
      macos: asset(release, /\.dmg$/i),
      windows: asset(release, /Setup\.exe$/i),
      linux: asset(release, /\.AppImage$/i),
    },
  });
}
