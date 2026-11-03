import assert from 'node:assert/strict';
import test from 'node:test';
import {loadPublishedDocs} from '../scripts/content.ts';

test('documentation has five ordered core guides', () => {
  const pages = loadPublishedDocs();
  assert.deepEqual(pages.map((page) => page.slug), ['introduction', 'installation', 'workspace', 'models-and-agents', 'skills-and-mcp']);
  assert.equal(new Set(pages.map((page) => page.slug)).size, pages.length);
  for (let index = 1; index < pages.length; index += 1) {
    const previous = pages[index - 1];
    const current = pages[index];
    assert.ok(
      current.sectionOrder > previous.sectionOrder
        || (current.sectionOrder === previous.sectionOrder && current.order >= previous.order),
    );
  }
});
