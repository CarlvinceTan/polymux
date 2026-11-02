import {strict as assert} from 'node:assert';
import {test} from 'node:test';
import {extractMemoryCitations} from './memoryCitations';

test('separates cited memories from reply prose and deduplicates them', () => {
  assert.deepEqual(extractMemoryCitations('An answer.\n\n<polymux-memories>["Short updates", "Short updates", "  Uses Svelte  ", 12]</polymux-memories>'), {text: 'An answer.', memories: ['Short updates', 'Uses Svelte']});
});
test('does not expose streaming or malformed metadata as prose', () => {
  for (const body of ['["Partial', 'bad JSON</polymux-memories>']) {
    assert.deepEqual(extractMemoryCitations(`Answer\n\n<polymux-memories>${body}`), {text: 'Answer', memories: []});
  }
});
test('preserves examples in code and ordinary replies', () => {
  for (const text of ['Plain answer', '```xml\n<polymux-memories>["Example"]</polymux-memories>\n```', '`<polymux-memories>["Example"]</polymux-memories>`']) {
    assert.deepEqual(extractMemoryCitations(text), {text, memories: []});
  }
});
