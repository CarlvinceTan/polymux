import test from 'node:test';
import assert from 'node:assert/strict';
import {chatSearchSnippets} from './chatSearchSnippets';

const messages = (...texts: string[]) => texts.map((text, index) => ({id: `message-${index}`, text}));

test('empty queries and title-only matches yield no snippets', () => {
  assert.deepEqual(chatSearchSnippets(messages('Interesting model choice'), ''), []);
  assert.deepEqual(chatSearchSnippets(undefined, 'inter'), []);
  assert.deepEqual(chatSearchSnippets(messages('Nothing relevant'), 'inter'), []);
});

test('matches retain source casing and the exact message id', () => {
  assert.deepEqual(chatSearchSnippets(messages('Nothing', 'Interpreting this?'), 'inter'), [
    {messageId: 'message-1', before: '', match: 'Inter', after: 'preting this?'},
  ]);
});

test('returns at most three distinct matching messages in order', () => {
  const hits = chatSearchSnippets(messages('First hit hit.', 'No match', 'Second hit.', 'Third hit.', 'Fourth hit.'), 'HIT');
  assert.deepEqual(hits.map((hit) => hit.messageId), ['message-0', 'message-2', 'message-3']);
  assert.deepEqual(hits.map((hit) => hit.match), ['hit', 'hit', 'hit']);
});

test('long messages retain highlighted context and navigation target', () => {
  const [snippet] = chatSearchSnippets(messages(`${'word '.repeat(40)}needle sits here ${'tail '.repeat(40)}`), 'needle');
  assert.ok(snippet);
  assert.equal(snippet.messageId, 'message-0');
  assert.equal(snippet.match, 'needle');
  assert.match(snippet.before, /…/);
  assert.match(snippet.after, /…/);
  assert.ok((snippet.before + snippet.match + snippet.after).length <= 80);
});
