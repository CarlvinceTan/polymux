import test from 'node:test';
import assert from 'node:assert/strict';
import {loadPublishedPosts} from '../scripts/content.ts';

test('loads published Markdown posts with stable blog metadata', () => {
  const posts = loadPublishedPosts();
  const comparison = posts.find(({slug}) => slug === 'polymux-vs-hermes-openclaw-khoj');

  assert.ok(comparison);
  assert.match(comparison.title, /Polymux vs Hermes Agent/);
  assert.equal(comparison.date, '2026-08-28');
  assert.ok(comparison.excerpt.length > 80);
});
