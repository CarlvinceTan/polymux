import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import test from 'node:test';
import {loadContent, checkGuide} from '../scripts/content.ts';

const config = JSON.parse(readFileSync(new URL('../vercel.json', import.meta.url), 'utf8'));

test('removed documentation URLs redirect only to existing guides and fragments', () => {
  const docs = loadContent().docs;
  for (const slug of ['browser', 'drive', 'hub', 'memory', 'privacy-and-permissions', 'quickstart', 'schedules-and-tasks', 'troubleshooting']) {
    const redirect = config.redirects.find((redirect) => redirect.source === `/docs/${slug}`);
    assert.ok(redirect, slug);
    assert.equal(redirect.permanent, true);
    checkGuide(redirect.destination, docs, slug);
  }
});

test('the plugin registry is served from polymux.com, read from the experimental plugins branch', () => {
  const route = config.rewrites.find((rewrite) => rewrite.source === '/plugins/index.json');
  assert.equal(route?.destination, 'https://raw.githubusercontent.com/CarlvinceTan/polymux/experimental/plugins/plugins/index.json');
  const headers = Object.fromEntries(config.headers.find((entry) => entry.source === '/plugins/index.json').headers.map(({key, value}) => [key, value]));
  assert.match(headers['Content-Type'], /^application\/json/);
});
