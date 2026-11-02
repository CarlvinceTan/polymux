import assert from 'node:assert/strict';
import { test } from 'node:test';
import { assetCache } from './assetCache';

test('concurrent requests share one load and cached values render immediately', async () => {
  let finish!: (value: object) => void;
  let calls = 0;
  const cache = assetCache({ cat: () => { calls++; return new Promise<object>((resolve) => { finish = resolve; }); } });
  const first = cache.load('cat');
  const second = cache.load('cat');
  assert.equal(first, second);
  assert.equal(cache.get('cat'), undefined);
  await Promise.resolve();
  const character = {};
  finish(character);
  assert.equal(await first, character);
  assert.equal(cache.get('cat'), character);
  assert.equal(await cache.load('cat'), character);
  assert.equal(calls, 1);
  assert.equal(cache.has('unknown'), false);
  assert.equal(await cache.load('unknown'), undefined);
});

test('failed loads can be retried and never populate the cache', async () => {
  let calls = 0;
  const cache = assetCache({ cat: async () => { if (++calls === 1) throw new Error('unavailable'); return 'ready'; } });
  await assert.rejects(cache.load('cat'), /unavailable/);
  assert.equal(cache.get('cat'), undefined);
  assert.equal(await cache.load('cat'), 'ready');
  assert.equal(cache.get('cat'), 'ready');
  assert.equal(calls, 2);
});
