import assert from 'node:assert/strict';
import {readFileSync, readdirSync} from 'node:fs';
import {resolve} from 'node:path';
import test from 'node:test';
import {JSDOM} from 'jsdom';
import {parse as parseYaml} from 'yaml';
import {docsContentRoot, loadPublishedDocs, siteRoot} from '../scripts/content.ts';
import {renderSafeMarkdown} from '../lib/content/markdown.js';

test('published guides render without screenshots and link to existing guides', () => {
  const slugs = new Set(loadPublishedDocs().map(({slug}) => slug));
  const browserWindow = new JSDOM('').window;
  try {
    for (const filename of readdirSync(docsContentRoot).filter((name) => name.endsWith('.md'))) {
      const source = readFileSync(resolve(docsContentRoot, filename), 'utf8');
      const [, metadata, body] = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
      if (parseYaml(metadata).published === false) continue;
      const document = new JSDOM(renderSafeMarkdown(body, browserWindow)).window;
      try {
        assert.equal(document.document.querySelectorAll('img').length, 0, filename);
        for (const link of document.document.querySelectorAll('a[href^="/docs/"]')) {
          const slug = link.getAttribute('href').split('/')[2] || 'introduction';
          assert.ok(slugs.has(slug), `${filename}: unknown guide ${slug}`);
        }
      } finally {
        document.close();
      }
    }
  } finally {
    browserWindow.close();
  }
});
