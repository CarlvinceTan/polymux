import assert from 'node:assert/strict';
import test from 'node:test';
import {inferQueuePriority, isDependentFollowUp, shouldSteerLiveTurn} from './queuePolicy.js';

test('ordinary follow-ups reach a main agent that has active delegated work', () => {
  assert.equal(shouldSteerLiveTurn({runId: 'run-1', immediate: false, hasActiveDelegation: true}), true);
});

test('explicit replacement language receives the attention lane', () => {
  assert.equal(inferQueuePriority('This is more urgent: check whether I need to respond to NUS today.'), 'attention');
  assert.equal(inferQueuePriority('Answer this first, then continue.'), 'attention');
});

test('ordinary urgency and background cues map conservatively', () => {
  assert.equal(inferQueuePriority('Please check this ASAP.'), 'urgent');
  assert.equal(inferQueuePriority('Do this in the background when you have time.'), 'background');
  assert.equal(inferQueuePriority('Find the latest NUS events for today.'), 'normal');
  assert.equal(inferQueuePriority('While that runs, check my latest messages.'), 'attention');
});

test('dependency cues remain distinct from unrelated prompts', () => {
  assert.equal(isDependentFollowUp('After that, draft the replies.'), true);
  assert.equal(isDependentFollowUp('Based on those results, make a shortlist.'), true);
  assert.equal(isDependentFollowUp('Find a good study spot near NUS.'), false);
});

test('ordinary follow-ups queue behind a run with no active delegation', () => {
  assert.equal(shouldSteerLiveTurn({runId: 'run-1', immediate: false, hasActiveDelegation: false}), false);
});

test('explicit immediate sends steer a live run but never a pending start', () => {
  assert.equal(shouldSteerLiveTurn({runId: 'run-1', immediate: true, hasActiveDelegation: false}), true);
  assert.equal(shouldSteerLiveTurn({runId: 'pending:assistant-1', immediate: true, hasActiveDelegation: true}), false);
});
