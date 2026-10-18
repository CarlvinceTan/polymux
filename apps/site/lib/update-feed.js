const REPOSITORY = 'CarlvinceTan/polymux';

export function isElectronRelease(tag) {
  return /^v0\.3\.\d+$/.test(tag);
}

function githubHeaders() {
  return {
    Accept: 'application/vnd.github+json',
    'User-Agent': 'Polymux-Updater',
    'X-GitHub-Api-Version': '2022-11-28',
  };
}

export async function updateFeed(platform, response) {
  const windows = platform === 'win32';
  const latest = await fetch(`https://api.github.com/repos/${REPOSITORY}/releases?per_page=100`, {
    headers: githubHeaders(),
  });
  if (!latest.ok) {
    response.setHeader('Cache-Control', 'no-store');
    return response.status(503).send('No public Polymux release is available yet.');
  }

  const releases = await latest.json();
  const release = releases.find((item) =>
    !item.draft && !item.prerelease && isElectronRelease(item.tag_name));
  if (!release) {
    response.setHeader('Cache-Control', 'no-store');
    return response.status(503).send('No public Polymux release is available yet.');
  }
  const manifestName = windows ? 'RELEASES' : 'latest-linux.yml';
  const manifest = release.assets?.find((item) => item.name === manifestName);
  if (!manifest) {
    response.setHeader('Cache-Control', 'no-store');
    return response.status(503).send(`The latest release has no ${windows ? 'Windows' : 'Linux'} update manifest.`);
  }

  const manifestResult = await fetch(manifest.browser_download_url, {
    headers: {'User-Agent': 'Polymux-Updater'},
  });
  if (!manifestResult.ok) {
    response.setHeader('Cache-Control', 'no-store');
    return response.status(502).send(`The ${windows ? 'Windows' : 'Linux'} update manifest could not be downloaded.`);
  }

  const assets = new Map(release.assets.map((item) => [item.name, item.browser_download_url]));
  const source = await manifestResult.text();
  const rewritten = windows
    ? source
      .split(/\r?\n/)
      .filter(Boolean)
      .map((line) => {
        const fields = line.trim().split(/\s+/);
        const assetUrl = assets.get(fields[1]);
        if (fields.length !== 3 || !assetUrl) return null;
        return `${fields[0]} ${assetUrl} ${fields[2]}`;
      })
      .filter(Boolean)
      .join('\n')
    : source.replace(/^(\s*-\s+url:\s+)(.+)$/m, (_line, prefix, encodedName) => {
      const name = decodeURIComponent(encodedName.trim().replace(/^['"]|['"]$/g, ''));
      return `${prefix}${assets.get(name) ?? encodedName}`;
    }).trim();

  if (!rewritten) {
    response.setHeader('Cache-Control', 'no-store');
    return response.status(503).send(`The ${windows ? 'Windows' : 'Linux'} update manifest has no downloadable packages.`);
  }

  response.setHeader('Content-Type', 'text/plain; charset=utf-8');
  response.setHeader('Cache-Control', 'public, s-maxage=300, stale-while-revalidate=60');
  return response.status(200).send(`${rewritten}\n`);
}
