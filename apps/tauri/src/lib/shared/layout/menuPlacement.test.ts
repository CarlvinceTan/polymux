import assert from 'node:assert/strict';
import test from 'node:test';
import {MENU_EDGE_MARGIN, clampToMenuEdge, fitInsetMenu} from './menuPlacement';

test('clampToMenuEdge keeps the box off both viewport sides', () => {
  assert.equal(clampToMenuEdge(10, 200, 1000), MENU_EDGE_MARGIN);
  assert.equal(clampToMenuEdge(100, 200, 1000), 100);
  assert.equal(clampToMenuEdge(900, 200, 1000), 1000 - MENU_EDGE_MARGIN - 200);
});

test('fitInsetMenu shrinks a full-width panel to respect edge margins', () => {
  assert.deepEqual(fitInsetMenu(220, {left: 0, width: 240}), {
    width: 240 - 2 * MENU_EDGE_MARGIN,
    left: MENU_EDGE_MARGIN,
  });
  assert.deepEqual(fitInsetMenu(180, {left: 0, width: 240}), {
    width: 180,
    left: 30,
  });
});
