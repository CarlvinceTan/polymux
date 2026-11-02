import assert from 'node:assert/strict';
import test from 'node:test';
import {
  MAX_CHAT_DRAWER_WIDTH,
  MAX_WORKSPACE_WIDTH,
  MIN_CHAT_DRAWER_WIDTH,
  MIN_WORKSPACE_WIDTH,
  chatDrawerOvershootMinimisesWorkspace,
  dockedMainMidX,
  workspaceOvershootExpands,
} from './layoutSizing';

test('chat-drawer overshoot does not minimise a docked workspace', () => {
  assert.equal(chatDrawerOvershootMinimisesWorkspace(900, 1300, false, 0), false);
  assert.equal(chatDrawerOvershootMinimisesWorkspace(900, 1300, false, MIN_WORKSPACE_WIDTH), false);
});

test('chat-drawer overshoot minimises only past max and into the window half', () => {
  const viewport = 1300;
  const half = viewport / 2;
  assert.equal(chatDrawerOvershootMinimisesWorkspace(MAX_CHAT_DRAWER_WIDTH, viewport, true, 0), false);
  assert.equal(chatDrawerOvershootMinimisesWorkspace(half - 1, viewport, true, 0), false);
  assert.equal(chatDrawerOvershootMinimisesWorkspace(half, viewport, true, 0), true);
  assert.equal(chatDrawerOvershootMinimisesWorkspace(half + 8, viewport, true, 0), true);
});

test('docked main-pane centre is the midpoint of the conversation column', () => {
  assert.equal(dockedMainMidX(1600, MIN_CHAT_DRAWER_WIDTH, MAX_WORKSPACE_WIDTH), MIN_CHAT_DRAWER_WIDTH + (1600 - MIN_CHAT_DRAWER_WIDTH - MAX_WORKSPACE_WIDTH) / 2);
  assert.equal(dockedMainMidX(1300, 0, MAX_WORKSPACE_WIDTH), (1300 - MAX_WORKSPACE_WIDTH) / 2);
});

test('workspace overshoot does not expand an already expanded workspace', () => {
  assert.equal(workspaceOvershootExpands(200, 1600, true, MIN_CHAT_DRAWER_WIDTH), false);
});

test('workspace overshoot falls back to the window half when no main-pane centre is given', () => {
  const viewport = 1600;
  const half = viewport / 2;
  const maxLeft = viewport - MAX_WORKSPACE_WIDTH;
  assert.equal(workspaceOvershootExpands(maxLeft, viewport, false, MIN_CHAT_DRAWER_WIDTH), false);
  assert.equal(workspaceOvershootExpands(half + 8, viewport, false, MIN_CHAT_DRAWER_WIDTH), false);
  assert.equal(workspaceOvershootExpands(half, viewport, false, MIN_CHAT_DRAWER_WIDTH), true);
  assert.equal(workspaceOvershootExpands(half - 8, viewport, false, MIN_CHAT_DRAWER_WIDTH), true);
});

test('workspace overshoot expands only past max and into the left half of the main pane', () => {
  const viewport = 1600;
  const maxLeft = viewport - MAX_WORKSPACE_WIDTH;
  const mainMid = dockedMainMidX(viewport, MIN_CHAT_DRAWER_WIDTH, MAX_WORKSPACE_WIDTH);
  const windowHalf = viewport / 2;
  assert.ok(mainMid < windowHalf);
  assert.equal(workspaceOvershootExpands(maxLeft, viewport, false, MIN_CHAT_DRAWER_WIDTH, mainMid), false);
  assert.equal(workspaceOvershootExpands(windowHalf, viewport, false, MIN_CHAT_DRAWER_WIDTH, mainMid), false);
  assert.equal(workspaceOvershootExpands(mainMid + 8, viewport, false, MIN_CHAT_DRAWER_WIDTH, mainMid), false);
  assert.equal(workspaceOvershootExpands(mainMid, viewport, false, MIN_CHAT_DRAWER_WIDTH, mainMid), true);
  assert.equal(workspaceOvershootExpands(mainMid - 8, viewport, false, MIN_CHAT_DRAWER_WIDTH, mainMid), true);
});

test('workspace overshoot can rest at max when that edge is still in the right half of the main pane', () => {
  const viewport = 1300;
  const maxLeft = viewport - MAX_WORKSPACE_WIDTH;
  const mainMid = dockedMainMidX(viewport, 0, MAX_WORKSPACE_WIDTH);
  assert.ok(maxLeft > mainMid);
  assert.equal(workspaceOvershootExpands(maxLeft, viewport, false, 0, mainMid), false);
  assert.equal(workspaceOvershootExpands(maxLeft - 1, viewport, false, 0, mainMid), false);
  assert.equal(workspaceOvershootExpands(mainMid, viewport, false, 0, mainMid), true);
});
