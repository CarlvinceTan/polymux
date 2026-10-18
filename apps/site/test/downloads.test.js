import assert from 'node:assert/strict';
import test from 'node:test';
import handler from '../api/downloads.js';

function response() {
  return {
    statusCode: 200,
    headers: {},
    body: null,
    setHeader(name, value) { this.headers[name] = value; },
    status(code) { this.statusCode = code; return this; },
    json(value) { this.body = value; return this; },
  };
}

test('maps the latest release to native installers for each platform', async (context) => {
  context.mock.method(globalThis, 'fetch', async () => ({
    ok: true,
    json: async () => [{
      tag_name: 'v0.3.0',
      published_at: '2026-08-23T00:00:00Z',
      html_url: 'https://github.com/CarlvinceTan/polymux/releases/tag/v0.3.0',
      assets: [
        {name: 'Polymux-0.3.0-arm64.dmg', browser_download_url: 'https://files/mac', size: 1},
        {name: 'Polymux-0.3.0-Setup.exe', browser_download_url: 'https://files/windows', size: 2},
        {name: 'Polymux-0.3.0-x86_64.AppImage', browser_download_url: 'https://files/appimage', size: 3},
      ],
    }],
  }));
  const result = response();
  await handler({}, result);
  assert.equal(result.statusCode, 200);
  assert.equal(result.body.version, '0.3.0');
  assert.equal(result.body.platforms.macos.url, 'https://files/mac');
  assert.equal(result.body.platforms.windows.url, 'https://files/windows');
  assert.equal(result.body.platforms.linux.url, 'https://files/appimage');
});

test('skips app-v releases when selecting the latest downloadable release', async (context) => {
  context.mock.method(globalThis, 'fetch', async () => ({
    ok: true,
    json: async () => [
      {
        tag_name: 'app-v0.4.0',
        draft: false,
        published_at: '2026-10-01T00:00:00Z',
        html_url: 'https://github.com/CarlvinceTan/polymux/releases/tag/app-v0.4.0',
        assets: [
          {name: 'Polymux-0.4.0-arm64.dmg', browser_download_url: 'https://files/mac-app', size: 1},
        ],
      },
      {
        tag_name: 'v0.3.4',
        draft: false,
        published_at: '2026-09-01T00:00:00Z',
        html_url: 'https://github.com/CarlvinceTan/polymux/releases/tag/v0.3.4',
        assets: [
          {name: 'Polymux-0.3.4-arm64.dmg', browser_download_url: 'https://files/mac', size: 1},
        ],
      },
    ],
  }));
  const result = response();
  await handler({}, result);
  assert.equal(result.statusCode, 200);
  assert.equal(result.body.version, '0.3.4');
});

test('strips a v release tag to a bare version for display', async (context) => {
  context.mock.method(globalThis, 'fetch', async () => ({
    ok: true,
    json: async () => [{tag_name: 'v0.3.4', draft: false, assets: []}],
  }));
  const result = response();
  await handler({}, result);
  assert.equal(result.statusCode, 200);
  assert.equal(result.body.version, '0.3.4');
});

test('resolves a requested version by its release tag', async (context) => {
  const requests = [];
  context.mock.method(globalThis, 'fetch', async (url) => {
    requests.push(String(url));
    return {
      ok: true,
      json: async () => ({
        tag_name: 'v0.2.2',
        published_at: '2026-08-28T00:00:00Z',
        html_url: 'https://github.com/CarlvinceTan/polymux/releases/tag/v0.2.2',
        assets: [{name: 'Polymux-0.2.2-arm64.dmg', browser_download_url: 'https://files/mac-0.2.2', size: 4}],
      }),
    };
  });
  const result = response();
  await handler({query: {version: '0.2.2'}}, result);
  assert.equal(result.statusCode, 200);
  assert.match(requests[0], /\/releases\/tags\/v0\.2\.2$/);
  assert.equal(result.body.version, '0.2.2');
  assert.equal(result.body.platforms.macos.url, 'https://files/mac-0.2.2');
  assert.equal(result.body.platforms.windows, null);
});

test('ignores a malformed version and serves the latest release', async (context) => {
  const requests = [];
  context.mock.method(globalThis, 'fetch', async (url) => {
    requests.push(String(url));
    return {ok: true, json: async () => [{tag_name: 'v0.3.0', assets: []}]};
  });
  const result = response();
  await handler({query: {version: '../../etc/passwd'}}, result);
  assert.equal(result.statusCode, 200);
  assert.match(requests[0], /\/releases\?per_page=10$/);
  assert.equal(result.body.version, '0.3.0');
});

test('rejects an unpublished version without spending GitHub API quota', async (context) => {
  const remote = context.mock.method(globalThis, 'fetch', async () => {
    throw new Error('fetch should not run');
  });
  const result = response();
  await handler({query: {version: '99.99.99'}}, result);
  assert.equal(result.statusCode, 404);
  assert.equal(remote.mock.callCount(), 0);
  assert.deepEqual(result.body, {error: 'That Polymux release is not published.'});
});

test('returns a bounded unavailable response before the first public release', async (context) => {
  context.mock.method(globalThis, 'fetch', async () => ({ok: false}));
  const result = response();
  await handler({}, result);
  assert.equal(result.statusCode, 503);
  assert.deepEqual(result.body, {error: 'No public Polymux release is available yet.'});
  assert.equal(result.headers['Cache-Control'], 'public, s-maxage=30');
});

test('bounds upstream failures instead of rejecting the request', async (context) => {
  context.mock.method(globalThis, 'fetch', async () => {
    throw new Error('network unavailable');
  });
  const result = response();
  await handler({}, result);
  assert.equal(result.statusCode, 503);
  assert.deepEqual(result.body, {error: 'No public Polymux release is available yet.'});
});
