import assert from 'node:assert/strict';
import test from 'node:test';
import {groupAvatarLayout} from './groupAvatarLayout';

test('keeps every Agent visible through four-member groups', () => {
  assert.deepEqual(groupAvatarLayout(1), {kind: 'single', visibleCount: 1, tileScale: 1});
  assert.deepEqual(groupAvatarLayout(2), {kind: 'pair', visibleCount: 2, tileScale: .72});
  assert.deepEqual(groupAvatarLayout(3), {kind: 'triple', visibleCount: 3, tileScale: .62});
  assert.deepEqual(groupAvatarLayout(4), {kind: 'quad', visibleCount: 4, tileScale: .58});
});

test('reserves a count badge only after the fourth Agent', () => {
  assert.deepEqual(groupAvatarLayout(5), {kind: 'triple', visibleCount: 3, tileScale: .62});
});
