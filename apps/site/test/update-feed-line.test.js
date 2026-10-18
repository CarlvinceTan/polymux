import assert from 'node:assert/strict';
import test from 'node:test';
import {isElectronRelease, updateFeed} from '../lib/update-feed.js';

test('accepts Electron v0.3.x release tags', () => {
  assert.equal(isElectronRelease('v0.3.4'), true);
});

test('rejects native and non-Electron tags', () => {
  assert.equal(isElectronRelease('app-v0.4.0'), false);
  assert.equal(isElectronRelease('v0.4.0'), false);
  assert.equal(isElectronRelease('v0.3.4-beta'), false);
});

test('selects the newest Electron release when a newer app release exists', async (context) => {
  const electronAssets = [
    {name: 'RELEASES', browser_download_url: 'https://files/RELEASES'},
    {name: 'polymux_desktop-0.3.5-full.nupkg', browser_download_url: 'https://files/full.nupkg'},
  ];
  context.mock.method(globalThis, 'fetch', async (url) => {
    if (String(url).includes('/releases?')) {
      return {
        ok: true,
        json: async () => [
          {tag_name: 'app-v0.4.0', draft: false, prerelease: false, assets: []},
          {tag_name: 'v0.3.5', draft: false, prerelease: false, assets: electronAssets},
          {tag_name: 'v0.3.4', draft: false, prerelease: false, assets: []},
        ],
      };
    }
    return {ok: true, text: async () => 'ABC polymux_desktop-0.3.5-full.nupkg 123\r\n'};
  });

  const result = {
    statusCode: 200,
    headers: {},
    body: null,
    setHeader(name, value) { this.headers[name] = value; },
    status(code) { this.statusCode = code; return this; },
    send(value) { this.body = value; return this; },
  };
  await updateFeed('win32', result);
  assert.equal(result.statusCode, 200);
  assert.equal(result.body, 'ABC https://files/full.nupkg 123\n');
});
