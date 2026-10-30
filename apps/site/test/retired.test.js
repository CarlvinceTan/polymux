import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {readFileSync, existsSync} from 'node:fs';
import test from 'node:test';
import mac from '../api/releases.ts';
import windows from '../api/updates/win32/x64/RELEASES.ts';
import linux from '../api/updates/linux/x64/latest-linux.yml.ts';
import install from '../api/install.ts';
import {response} from './helpers.js';

test('every retired updater returns 410 without contacting GitHub', (context) => {
  const upstream = context.mock.method(globalThis, 'fetch', () => {throw new Error('must not contact GitHub');});
  for (const handler of [mac, windows, linux]) {
    const result = response();
    handler({}, result);
    assert.equal(result.statusCode, 410);
    assert.match(result.json().error, /retired/);
    assert.match(result.json().error, /docs\/installation/);
  }
  assert.equal(upstream.mock.callCount(), 0);
});

test('the old installer URL returns a non-installing shell-safe retirement response', () => {
  const result = response();
  install({}, result);
  assert.equal(result.statusCode, 410);
  assert.doesNotMatch(result.body, /curl|tar |mkdir|install -m/);
  assert.equal(existsSync(new URL('../public/install.sh', import.meta.url)), false);
  const config = JSON.parse(readFileSync(new URL('../vercel.json', import.meta.url), 'utf8'));
  assert.ok(config.rewrites.some(({source, destination}) => source === '/install.sh' && destination === '/api/install'));
  if (process.platform !== 'win32') {
    const shell = spawnSync('sh', ['-s'], {input: result.body, encoding: 'utf8'});
    assert.equal(shell.status, 1);
    assert.match(shell.stderr, /retired/);
  }
});
