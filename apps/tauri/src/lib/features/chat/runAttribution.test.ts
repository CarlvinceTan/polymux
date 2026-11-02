import assert from 'node:assert/strict';
import test from 'node:test';
import {addConversationRun, bindPendingRun, latestConversationRun, removeConversationRun} from './runAttribution';

test('keeps concurrent runs ordered and settles only the named run', () => {
  let runs = addConversationRun({}, 'chat', 'run-a');
  runs = addConversationRun(runs, 'chat', 'run-b');
  assert.equal(latestConversationRun(runs, 'chat'), 'run-b');

  runs = removeConversationRun(runs, 'chat', 'run-a');
  assert.deepEqual(runs.chat, ['run-b']);
  assert.equal(latestConversationRun(runs, 'chat'), 'run-b');
});

test('binds a pending assistant row to exactly its returned run', () => {
  const result = bindPendingRun(
    {chat: ['pending:assistant-a', 'run-b']},
    {'pending:assistant-a': 'assistant-a', 'run-b': 'assistant-b'},
    'chat',
    'pending:assistant-a',
    'run-a',
  );
  assert.deepEqual(result.runs.chat, ['run-a', 'run-b']);
  assert.deepEqual(result.assistants, {'run-a': 'assistant-a', 'run-b': 'assistant-b'});
});

test('removing the last run removes only that conversation bucket', () => {
  assert.deepEqual(removeConversationRun({chat: ['run-a'], other: ['run-c']}, 'chat', 'run-a'), {other: ['run-c']});
});
