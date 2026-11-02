import assert from 'node:assert/strict';
import test from 'node:test';
import type {TeamAvatarDto} from '@polymux/protocol';
import {
  adaptiveMonochromeAvatar,
  BLOUB_ADAPTIVE_MONOCHROME,
  bloubColorForTheme,
  isAdaptiveMonochromeAvatar,
  normalizeBloubAvatar,
} from './colors';

const cream: TeamAvatarDto = {shape: 'circle', color: BLOUB_ADAPTIVE_MONOCHROME.dark};

test('adaptive monochrome is explicit durable avatar data', () => {
  const adaptive = adaptiveMonochromeAvatar(cream);
  assert.equal(isAdaptiveMonochromeAvatar(cream), false);
  assert.equal(isAdaptiveMonochromeAvatar(adaptive), true);
  assert.equal(bloubColorForTheme(adaptive, 'light'), BLOUB_ADAPTIVE_MONOCHROME.light);
  assert.equal(bloubColorForTheme(adaptive, 'dark'), BLOUB_ADAPTIVE_MONOCHROME.dark);
});

test('normalizes the monochrome choice while keeping coloured avatars fixed', () => {
  assert.equal(normalizeBloubAvatar(cream), cream);
  assert.deepEqual(normalizeBloubAvatar(adaptiveMonochromeAvatar(cream)), adaptiveMonochromeAvatar(cream));
  const violet: TeamAvatarDto = {shape: 'triangle', color: '#8b5cf6'};
  assert.equal(normalizeBloubAvatar(violet), violet);
  assert.equal(bloubColorForTheme(violet, 'light'), violet.color);
  assert.equal(bloubColorForTheme(violet, 'dark'), violet.color);
});
