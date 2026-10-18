import {isElectronRelease} from '../lib/update-feed.js';

const REPOSITORY = 'CarlvinceTan/polymux';

export default async function handler(_request, response) {
  const result = await fetch(`https://api.github.com/repos/${REPOSITORY}/releases?per_page=100`, {
    headers: {
      Accept: 'application/vnd.github+json',
      'User-Agent': 'Polymux-Updater',
      'X-GitHub-Api-Version': '2022-11-28',
    },
  });

  if (!result.ok) {
    response.setHeader('Cache-Control', 'no-store');
    return response.status(503).json({error: 'No public Polymux release is available yet.'});
  }

  const releases = await result.json();
  const release = releases.find((item) =>
    !item.draft && !item.prerelease && isElectronRelease(item.tag_name));
  if (!release) {
    response.setHeader('Cache-Control', 'no-store');
    return response.status(503).json({error: 'No public Polymux release is available yet.'});
  }
  const asset = release.assets?.find((item) =>
    /darwin-arm64.*\.zip$/i.test(item.name) || /arm64.*\.zip$/i.test(item.name),
  );

  if (!asset) {
    response.setHeader('Cache-Control', 'no-store');
    return response.status(503).json({error: 'The latest release has no macOS update archive.'});
  }

  response.setHeader('Cache-Control', 'public, s-maxage=300, stale-while-revalidate=60');
  return response.status(200).json({
    url: asset.browser_download_url,
    name: release.tag_name.replace(/^v/, ''),
    notes: release.body ?? '',
    pub_date: release.published_at,
  });
}
