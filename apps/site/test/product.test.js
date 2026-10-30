import assert from 'node:assert/strict';
import test from 'node:test';
import {loadProductFeatures} from '../scripts/content.ts';

test('product features are grouped, unique, and written for people', () => {
  const features = loadProductFeatures();
  assert.equal(features.length, 8);
  assert.equal(features.filter(({group}) => group === 'app').length, 4);
  assert.equal(features.filter(({group}) => group === 'agent').length, 4);
  assert.equal(new Set(features.map(({slug}) => slug)).size, features.length);

  for (const feature of features) {
    assert.ok(feature.menuDescription.length >= 25);
    assert.ok(feature.intro.length >= 120);
    assert.equal(feature.benefits.length, 3);
    assert.equal(feature.steps.length, 3);
    assert.match(feature.docsPath, /^\/docs\/[a-z0-9-]+\/(?:#[a-z0-9-]+)?$/);
  }
});
