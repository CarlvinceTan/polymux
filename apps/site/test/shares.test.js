import assert from 'node:assert/strict';
import {existsSync, readFileSync, readdirSync} from 'node:fs';
import test from 'node:test';

const site = new URL('../', import.meta.url);
test('retired conversation links use the normal hosting not-found response', () => {
  const config = JSON.parse(readFileSync(new URL('vercel.json', site), 'utf8'));
  assert.deepEqual(config.rewrites ?? [], []);
  assert.deepEqual(config.redirects ?? [], []);
  assert.equal(existsSync(new URL('share', site)) && readdirSync(new URL('share', site)).length > 0, false);
  assert.equal(readdirSync(new URL('api', site)).some(name => /^shares\./.test(name)), false);
  assert.doesNotMatch(readFileSync(new URL('vite.config.ts', site), 'utf8'), /share:/);
});

test('privacy policy does not offer hosted conversation links', () => {
  const policy = readFileSync(new URL('privacy-policy/index.html', site), 'utf8');
  assert.match(policy, /does not host public conversation links/);
  assert.doesNotMatch(policy, /snapshot|Redis|24 hours/i);
});
