import assert from 'node:assert/strict';
import test from 'node:test';
import { mentionChoices, mentionQuery, mentionTargets, updateMentions, validMention } from './mentions';

const token = { id: 'alpha', text: '@Éva', location: 3, length: 4 };

test('UTF-16 edits move or invalidate exact mention spans', () => {
  const original = '😀 @Éva hello';
  assert.equal(validMention(token, original), true);
  assert.deepEqual(updateMentions([token], original, 'X' + original, { location: 0, length: 0 }), [{ ...token, location: 4 }]);
  assert.deepEqual(updateMentions([token], original, '😀 @Évx hello', { location: 6, length: 1 }), []);
  assert.deepEqual(updateMentions([token], original, '😀 @ÉvXa hello', { location: 6, length: 0 }), []);
  assert.deepEqual(updateMentions([token], original, '😀 @Éva_hello', { location: 7, length: 1 }), []);
  assert.equal(validMention({ ...token, location: 1 }, original), false);
});

test('ambiguous repeated edits discard any possibly touched token', () => {
  const text = '@A @A';
  const tokens = [{ id: 'first', text: '@A', location: 0, length: 2 }, { id: 'second', text: '@A', location: 3, length: 2 }];
  assert.deepEqual(updateMentions(tokens, text, '@A'), []);
  assert.deepEqual(updateMentions(tokens, text, '@A', { location: 0, length: 3 }), [{ ...tokens[1], location: 0 }]);
});

test('targets remain distinct, validate membership and respect Unicode boundaries', () => {
  const text = '@Éva @Éva';
  const one = { id: 'alpha', text: '@Éva', location: 0, length: 4 };
  const two = { ...one, location: 5 };
  assert.deepEqual(mentionTargets([one, two], text, new Set(['alpha'])), ['alpha']);
  assert.equal(mentionTargets([one], text, new Set()), undefined);
  assert.equal(validMention(one, '@Évaline'), false);
});

test('picker matches and ranks folded names and honors query boundaries', () => {
  const members = [{ id: 'a', name: 'Éva', role: '', model: '', active: true }, { id: 'b', name: 'Eva Lee', role: '', model: '', active: true }];
  assert.deepEqual(mentionChoices(members, 'eva').map((item) => item.id), ['a', 'b']);
  assert.deepEqual(mentionQuery('Hi (@ev', [], 2), { start: 4, term: 'ev' });
  assert.equal(mentionQuery('email@ev', [], 2), undefined);
  assert.equal(mentionQuery('Hi @ev\n', [], 2), undefined);
  assert.equal(mentionQuery('Hi @ev', [], 1), undefined);
});
