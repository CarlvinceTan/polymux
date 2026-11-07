import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import test from 'node:test';
import {createAppcast} from '../lib/server/appcast.ts';
import {loadContent} from '../scripts/content.ts';
import {response} from './helpers.js';

const entry = {version: '1.0.0', title: 'Native release', date: '2026-10-24', summary: 'Native app', downloadable: true, historical: false, tag: 'app-v1.0.0'};
const feed = '<?xml version="1.0" encoding="utf-8"?>\n<rss version="2.0"><channel><title>Polymux</title></channel></rss>';
const remote = (overrides = {}) => ({
  tag_name: entry.tag,
  assets: [{name: 'appcast.xml', browser_download_url: 'https://files.example/appcast.xml'}],
  ...overrides,
});
const json = (value) => new Response(JSON.stringify(value), {headers: {'Content-Type': 'application/json'}});
const xml = (body = feed) => ({ok: true, text: async () => body});

test('serves the signed feed attached to the newest native release', async () => {
  const requests = [];
  const appcast = createAppcast([entry], {
    request: async (url) => {requests.push(String(url)); return json(remote());},
    download: async (url) => {requests.push(String(url)); return xml();},
  });
  const result = response();
  await appcast({}, result);
  assert.equal(result.statusCode, 200);
  assert.equal(result.headers['Content-Type'], 'application/xml; charset=utf-8');
  assert.match(result.body, /<rss/);
  assert.match(requests[0], /\/releases\/tags\/app-v1\.0\.0$/);
  assert.equal(requests[1], 'https://files.example/appcast.xml');
});

test('skips historical, unpublished, and prerelease entries when selecting the feed', async () => {
  let calls = 0;
  const appcast = createAppcast([
    {...entry, version: '3.0.0', historical: true},
    {...entry, version: '2.0.0', downloadable: false},
    {...entry, version: '1.1.0-beta.1'},
    entry,
  ], {request: async () => {calls++; return json(remote());}, download: async () => xml()});
  const result = response();
  await appcast({}, result);
  assert.equal(result.statusCode, 200);
  assert.equal(calls, 1);
});

test('reports unavailable feeds without returning stale XML', async () => {
  for (const value of [remote({draft: true}), remote({tag_name: 'app-v9.0.0'}), remote({assets: []}), null, {}]) {
    const result = response();
    await createAppcast([entry], {request: async () => json(value), download: async () => xml()})({}, result);
    assert.equal(result.statusCode, 503);
  }
  for (const download of [async () => ({ok: false, text: async () => feed}), async () => xml('not a feed')]) {
    const result = response();
    await createAppcast([entry], {request: async () => json(remote()), download})({}, result);
    assert.equal(result.statusCode, 502);
  }
  const failed = response();
  await createAppcast([entry], {request: async () => {throw new Error('offline');}, download: async () => xml()})({}, failed);
  assert.equal(failed.statusCode, 503);
});

test('an empty release list answers 503 without spending upstream quota', async () => {
  const result = response();
  await createAppcast([], {request: async () => {throw new Error('unexpected request');}, download: async () => xml()})({}, result);
  assert.equal(result.statusCode, 503);
  assert.match(result.body, /not published/);
});

test('the production adapter follows prepared release metadata', async (context) => {
  const current = loadContent().releases.find((release) => release.downloadable && !release.historical);
  assert.ok(current, 'Publish the native release before enabling updates');
  const requests = [];
  context.mock.method(globalThis, 'fetch', async (url) => {
    requests.push(String(url));
    return String(url).startsWith('https://api.github.com/')
      ? json(remote({tag_name: current.tag}))
      : new Response(feed);
  });
  const {default: handler} = await import('../api/appcast.ts');
  const result = response();
  await handler({}, result);
  assert.equal(result.statusCode, 200);
  assert.equal(result.body, `${feed}\n`);
  assert.deepEqual(requests, [
    `https://api.github.com/repos/CarlvinceTan/polymux/releases/tags/${current.tag}`,
    'https://files.example/appcast.xml',
  ]);
});

test('the site rewrites the stable Sparkle feed URL to the function', () => {
  const config = JSON.parse(readFileSync(new URL('../vercel.json', import.meta.url), 'utf8'));
  assert.ok(config.rewrites.some((rewrite) => rewrite.source === '/appcast.xml' && rewrite.destination === '/api/appcast'));
});
