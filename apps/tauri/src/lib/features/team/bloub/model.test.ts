import assert from 'node:assert/strict';
import test from 'node:test';
import {TEAM_AVATAR_EXPRESSIONS, TEAM_AVATAR_SHAPES} from '@polymux/protocol';
import {
  BLOUB_COLORS,
  BLOUB_EXPRESSIONS,
  BLOUB_PROFILE_SAMPLES,
  BLOUB_SHAPES,
  bloubEyePoses,
  bloubVerticalCenter,
  renderBloubFrame,
} from './model';

test('exposes Bloub’s complete customization catalogues in their tested order', () => {
  assert.deepEqual(BLOUB_SHAPES.map(({id}) => id), [...TEAM_AVATAR_SHAPES]);
  assert.deepEqual(BLOUB_EXPRESSIONS.map(({id}) => id), [...TEAM_AVATAR_EXPRESSIONS]);
  assert.deepEqual(BLOUB_COLORS.map(({hex}) => hex), [
    '#0a0a0c', '#8b5e3c', '#e8483f', '#f08a24', '#f0b429', '#3ecf8e',
    '#2fbfa0', '#3b93f0', '#8b5cf6', '#e152b0', '#a3a3a3', '#f1efe9',
  ]);
  assert.equal(BLOUB_SHAPES.length, 9);
  assert.equal(BLOUB_COLORS.length, 12);
  assert.equal(BLOUB_EXPRESSIONS.length, 16);
});

test('keeps every radial shape finite, positive, normalized, and distinct', () => {
  const signatures = new Set<string>();
  for (const shape of BLOUB_SHAPES) {
    assert.equal(shape.radii.length, BLOUB_PROFILE_SAMPLES, shape.id);
    assert.ok(shape.radii.every((radius) => Number.isFinite(radius) && radius > 0), shape.id);
    assert.ok(Math.max(...shape.radii) <= 1.151, shape.id);
    signatures.add(shape.radii.map((radius) => radius.toFixed(5)).join(','));
  }
  assert.equal(signatures.size, BLOUB_SHAPES.length);
});

test('centres static silhouettes without moving symmetric shapes', () => {
  assert.ok(bloubVerticalCenter('triangle') < -10);
  assert.ok(Math.abs(bloubVerticalCenter('circle')) < .001);
  for (const shape of BLOUB_SHAPES) {
    const ys = shape.radii.map((radius, index) =>
      Math.sin(index / BLOUB_PROFILE_SAMPLES * Math.PI * 2) * radius * 100);
    const center = bloubVerticalCenter(shape.id);
    assert.ok(Math.abs(Math.min(...ys) + Math.max(...ys) - 2 * center) < .001, shape.id);
  }
});

test('retains Bloub’s measured spherical-eye projection', () => {
  const [inner, outer] = bloubEyePoses({yaw: 28.49, pitch: 28.62, roll: -13}, 1);
  assert.ok(Math.abs(outer.depth / inner.depth - .663) < .08);
  for (const gaze of [
    {yaw: 28.49, pitch: 28.62, roll: -13},
    {yaw: -40, pitch: 10, roll: 5},
    {yaw: 0, pitch: 0, roll: 0},
  ]) {
    const [left, right] = bloubEyePoses(gaze, 1);
    const dot = left.x * right.x + left.y * right.y + left.depth * right.depth;
    const separation = Math.acos(dot) * 180 / Math.PI;
    assert.ok(Math.abs(separation - 30.92) < .001);
  }
});

test('renders every shape-expression combination as deterministic mask geometry', () => {
  const bodies = new Set<string>();
  for (const shape of TEAM_AVATAR_SHAPES) {
    for (const expression of TEAM_AVATAR_EXPRESSIONS) {
      const avatar = {shape, expression, color: '#8b5cf6'};
      const first = renderBloubFrame(avatar, 1, false);
      const second = renderBloubFrame(avatar, 1, false);
      assert.deepEqual(second, first);
      assert.match(first.bodyPath, /^M.+Z$/);
      assert.equal(first.eyes.length, 2, `${shape}/${expression}`);
      for (const eye of first.eyes) {
        assert.match(eye.path, /^M.+Z$/);
        assert.match(eye.matrix, /^matrix\(-?[\d.]+,-?[\d.]+,-?[\d.]+,-?[\d.]+,-?[\d.]+,-?[\d.]+\)$/);
        assert.ok(eye.opacity > 0 && eye.opacity <= 1);
      }
    }
    bodies.add(renderBloubFrame({shape, expression: 'neutral', color: '#0a0a0c'}, 1, false).bodyPath);
  }
  assert.equal(bodies.size, TEAM_AVATAR_SHAPES.length);
});

test('animates deterministically while a frozen customization tile stays still', () => {
  const avatar = {shape: 'cloud' as const, expression: 'curious' as const, color: '#3b93f0'};
  assert.notDeepEqual(renderBloubFrame(avatar, .5, true), renderBloubFrame(avatar, 2, true));
  assert.deepEqual(renderBloubFrame(avatar, .5, false), renderBloubFrame(avatar, 2, false));
});
