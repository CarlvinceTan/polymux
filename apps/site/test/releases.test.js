import assert from 'node:assert/strict';
import test from 'node:test';
import macHandler from '../api/releases.js';
import windowsHandler from '../api/updates/win32/x64/RELEASES.js';
import linuxHandler from '../api/updates/linux/x64/latest-linux.yml.js';

function response() {
  return {
    statusCode: 200,
    headers: {},
    body: null,
    setHeader(name, value) { this.headers[name] = value; },
    status(code) { this.statusCode = code; return this; },
    send(value) { this.body = value; return this; },
    json(value) { this.body = value; return this; },
  };
}

test('serves the newest Electron release for the macOS update feed', async (context) => {
  const requests = [];
  context.mock.method(globalThis, 'fetch', async (url) => {
    requests.push(String(url));
    return {
      ok: true,
      json: async () => [
        {
          tag_name: 'app-v0.4.0',
          draft: false,
          prerelease: false,
          published_at: '2026-10-01T00:00:00Z',
          body: 'native',
          assets: [
            {name: 'Polymux-0.4.0-darwin-arm64.zip', browser_download_url: 'https://files/mac-app.zip'},
          ],
        },
        {
          tag_name: 'v0.3.4',
          draft: false,
          prerelease: false,
          published_at: '2026-09-01T00:00:00Z',
          body: 'electron',
          assets: [
            {name: 'Polymux-0.3.4-darwin-arm64.zip', browser_download_url: 'https://files/mac.zip'},
          ],
        },
      ],
    };
  });

  const result = response();
  await macHandler({}, result);
  assert.equal(result.statusCode, 200);
  assert.match(requests[0], /\/releases\?per_page=100$/);
  assert.equal(result.body.name, '0.3.4');
  assert.equal(result.body.url, 'https://files/mac.zip');
});

test('serves a Squirrel.Windows manifest with absolute GitHub asset URLs', async (context) => {
  const assets = [
    {name: 'RELEASES', browser_download_url: 'https://files/RELEASES'},
    {name: 'polymux_desktop-0.3.0-full.nupkg', browser_download_url: 'https://files/full.nupkg'},
  ];
  context.mock.method(globalThis, 'fetch', async (url) => {
    if (String(url).includes('/releases?')) {
      return {ok: true, json: async () => [{tag_name: 'v0.3.0', draft: false, prerelease: false, assets}]};
    }
    return {ok: true, text: async () => 'ABC polymux_desktop-0.3.0-full.nupkg 123\r\n'};
  });

  const result = response();
  await windowsHandler({}, result);
  assert.equal(result.statusCode, 200);
  assert.equal(result.body, 'ABC https://files/full.nupkg 123\n');
  assert.equal(result.headers['Content-Type'], 'text/plain; charset=utf-8');
});

test('serves Linux updater metadata with an absolute AppImage URL', async (context) => {
  const assets = [
    {name: 'latest-linux.yml', browser_download_url: 'https://files/latest-linux.yml'},
    {name: 'Polymux-0.3.0-x86_64.AppImage', browser_download_url: 'https://files/Polymux.AppImage'},
  ];
  context.mock.method(globalThis, 'fetch', async (url) => {
    if (String(url).includes('/releases?')) {
      return {ok: true, json: async () => [{tag_name: 'v0.3.0', draft: false, prerelease: false, assets}]};
    }
    return {
      ok: true,
      text: async () => 'version: 0.3.0\nfiles:\n  - url: Polymux-0.3.0-x86_64.AppImage\n    sha512: abc\n    size: 123\n',
    };
  });

  const result = response();
  await linuxHandler({}, result);
  assert.equal(result.statusCode, 200);
  assert.match(result.body, /url: https:\/\/files\/Polymux\.AppImage/);
});
