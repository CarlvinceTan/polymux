import assert from 'node:assert/strict';
import { test } from 'node:test';
import { DictationDraft } from './dictationDraft';

const cursor = (location: number) => ({ location, length: 0 });

test('partial speech replaces only its segment and ignores late revisions', () => {
  const draft = new DictationDraft();
  const first = draft.update('hi', 0, false, 'before after', cursor(7))!;
  const final = draft.update('hello ', 0, true, first.text, cursor(first.cursor))!;
  assert.equal(final.text, 'before hello after');
  assert.equal(draft.update('late', 0, true, final.text, cursor(final.cursor)), undefined);
  assert.equal(draft.update('again ', 1, true, final.text, cursor(final.cursor))?.text, 'before hello again after');
});

test('draft edits preserve the user’s changes and UTF-16 selections', () => {
  const draft = new DictationDraft();
  const first = draft.update('old', 0, false, '👋 end', cursor(3))!;
  draft.edit(first.text, 'X' + first.text);
  const revised = draft.update('new', 0, false, 'X' + first.text, cursor(0))!;
  assert.equal(revised.text, 'X👋 newend');
  draft.edit(revised.text, 'X👋 typedend');
  assert.equal(draft.update('late', 0, true, 'X👋 typedend', cursor(0)), undefined);
  draft.stop();
  assert.equal(draft.update('x', 1, true, '👋', cursor(1)), undefined);
});

test('an empty final retracts the partial and stop begins a new session', () => {
  const draft = new DictationDraft();
  const partial = draft.update('wrong', 0, false, 'typed end', cursor(6))!;
  const final = draft.update('', 0, true, partial.text, cursor(partial.cursor))!;
  assert.equal(final.text, 'typed end');
  assert.equal(draft.update('late', 0, true, final.text, cursor(final.cursor)), undefined);
  draft.stop();
  assert.equal(draft.update('new', 0, true, final.text, cursor(final.cursor))?.text, 'typed newend');
});
