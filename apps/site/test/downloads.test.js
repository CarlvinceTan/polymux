import assert from 'node:assert/strict';
import test from 'node:test';
import {createDownloads} from '../lib/server/downloads.ts';
import {loadContent} from '../scripts/content.ts';
import {visiblePlatforms} from '../src/lib/platform.ts';
import {response} from './helpers.js';

const entry = {version: '1.0.0', title: 'Native release', date: '2026-10-24', summary: 'Native app', downloadable: true, historical: false, tag: 'app-v1.0.0'};
const remote = (overrides = {}) => ({tag_name: entry.tag, html_url: `https://github.com/CarlvinceTan/polymux/releases/tag/${entry.tag}`, assets: [
  {name: 'Polymux-1.0.0-arm64.dmg', browser_download_url: 'https://files.example/mac', size: 1},
  {name: 'Polymux-1.0.0-Setup.exe', browser_download_url: 'https://files.example/windows', size: 2},
  {name: 'Polymux-1.0.0-x86_64.AppImage', browser_download_url: 'https://files.example/linux', size: 3},
], ...overrides});
const json = (value) => new Response(JSON.stringify(value), {headers: {'Content-Type': 'application/json'}});

test('uses the published native tag directly and maps installers for each platform', async () => {
  const requests = [];
  const download = createDownloads([entry], {request: async (url) => {requests.push(String(url)); return json(remote());}});
  const result = response();
  await download({}, result);
  assert.equal(result.statusCode, 200);
  assert.equal(result.json().version, '1.0.0');
  assert.equal(result.json().platforms.macos.url, 'https://files.example/mac');
  assert.equal(result.json().platforms.windows.url, 'https://files.example/windows');
  assert.equal(result.json().platforms.linux.url, 'https://files.example/linux');
  assert.match(requests[0], /\/releases\/tags\/app-v1\.0\.0$/);
  assert.ok(!requests[0].includes('per_page'));
});

test('a requested version resolves its own published tag', async () => {
  const requests = [];
  const older = {...entry, version: '0.9.0', tag: 'v0.9.0'};
  const download = createDownloads([entry, older], {request: async (url) => {requests.push(String(url)); return json(remote({tag_name: older.tag}));}});
  const result = response();
  await download({query: {version: 'v0.9.0'}}, result);
  assert.equal(result.statusCode, 200);
  assert.match(requests[0], /tags\/v0\.9\.0$/);
  assert.equal(result.json().version, '0.9.0');
});

test('invalid, unpublished, historical, and unavailable versions spend no upstream quota', async () => {
  let calls = 0;
  const download = createDownloads([entry, {...entry, version: '0.3.0', historical: true}, {...entry, version: '0.2.0', downloadable: false}], {request: async () => {calls++; throw new Error('unexpected request');}});
  for (const [version, status] of [['../../passwd', 400], ['99.0.0', 404], ['0.3.0', 404], ['0.2.0', 404]]) {
    const result = response();
    await download({query: {version}}, result);
    assert.equal(result.statusCode, status);
  }
  assert.equal(calls, 0);
});

test('latest selection skips unpublished downloads, historical releases, and prereleases', async () => {
  const download = createDownloads([
    {...entry, version: '3.0.0', historical: true}, {...entry, version: '2.0.0', downloadable: false},
    {...entry, version: '1.1.0-beta.1'}, entry,
  ], {request: async () => json(remote())});
  const result = response();
  await download({}, result);
  assert.equal(result.statusCode, 200);
  assert.equal(result.json().version, entry.version);
});

test('draft, mismatched, malformed, server-only, and failed upstream releases are unavailable', async () => {
  for (const value of [remote({draft: true}), remote({prerelease: true}), remote({tag_name: 'server-v9.0.0'}), remote({assets: []}), null, {}]) {
    const result = response();
    await createDownloads([entry], {request: async () => json(value)})({}, result);
    assert.equal(result.statusCode, 503);
  }
  for (const request of [async () => {throw new Error('offline');}, async () => new Response('invalid JSON')]) {
    const result = response();
    await createDownloads([entry], {request})({}, result);
    assert.equal(result.statusCode, 503);
  }
});

test('the deadline covers stalled headers and stalled response bodies', async () => {
  for (const request of [() => new Promise(() => {}), async () => ({ok: true, json: () => new Promise(() => {})})]) {
    const result = response();
    await createDownloads([entry], {request, timeoutMs: 10})({}, result);
    assert.equal(result.statusCode, 503);
  }
});

test('successful upstream reads are coalesced and cached', async () => {
  let calls = 0;
  const download = createDownloads([entry], {request: async () => {calls++; return json(remote());}});
  await Promise.all([download({}, response()), download({}, response())]);
  await download({}, response());
  assert.equal(calls, 1);
});

test('the production adapter follows freshly prepared release metadata', async (context) => {
  const current = loadContent().releases.find((release) => release.downloadable && !release.historical);
  assert.ok(current, 'Publish the native release before enabling downloads');
  const requests = [];
  context.mock.method(globalThis, 'fetch', async (url) => {
    requests.push(String(url));
    return json(remote({tag_name: current.tag, assets: [{
      name: `Polymux-${current.version}-arm64.dmg`, browser_download_url: 'https://files.example/mac', size: 1,
    }]}));
  });
  const {default: handler} = await import('../api/downloads.ts');
  const result = response();
  await handler({}, result);
  assert.equal(result.statusCode, 200);
  assert.equal(result.json().version, current.version);
  assert.equal(result.json().platforms.macos.name, `Polymux-${current.version}-arm64.dmg`);
  assert.deepEqual(requests, [`https://api.github.com/repos/CarlvinceTan/polymux/releases/tags/${current.tag}`]);
});

test('download cards only show platforms the release actually ships', () => {
  const asset = {name: 'Polymux-1.0.0-arm64.dmg', url: 'https://files.example/mac', size: 1};
  assert.deepEqual(visiblePlatforms(null), ['macos']);
  assert.deepEqual(visiblePlatforms({
    version: '1.0.0', releaseUrl: 'https://github.com/CarlvinceTan/polymux/releases/tag/app-v1.0.0',
    platforms: {macos: asset, windows: null, linux: null},
  }), ['macos']);
  assert.deepEqual(visiblePlatforms({
    version: '1.0.0', releaseUrl: 'https://github.com/CarlvinceTan/polymux/releases/tag/app-v1.0.0',
    platforms: {macos: asset, windows: asset, linux: asset},
  }), ['macos', 'windows', 'linux']);
});
