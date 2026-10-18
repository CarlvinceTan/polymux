import assert from 'node:assert/strict';
import {readFileSync, readdirSync} from 'node:fs';
import {resolve} from 'node:path';
import test from 'node:test';
import {JSDOM} from 'jsdom';
import {parse} from 'svelte/compiler';
import {parse as parseYaml} from 'yaml';
import {docsContentRoot, loadPublishedDocs} from '../scripts/docs-content.mjs';
import {loadProductFeatures, siteRoot} from '../scripts/product-content.mjs';
import {renderSafeMarkdown} from '../src/lib/markdown.js';

function images(node) {
  if (!node || typeof node !== 'object') return [];
  return [
    ...(node.type === 'RegularElement' && node.name === 'img' ? [node] : []),
    ...Object.values(node).flatMap((value) => Array.isArray(value) ? value.flatMap(images) : images(value)),
  ];
}

test('home and product templates contain branding images only, not app screenshots', () => {
  for (const filename of ['App.svelte', 'ProductPage.svelte']) {
    const source = readFileSync(resolve(siteRoot, 'src', filename), 'utf8');
    const ast = parse(source, {modern: true});
    for (const image of images(ast.fragment)) {
      const src = image.attributes.find((attribute) => attribute.name === 'src')?.value;
      assert.equal(src?.type, 'ExpressionTag', `${filename}: unexpected image source`);
      assert.equal(src.expression.type, 'Identifier', `${filename}: unexpected image expression`);
      assert.ok(['logo', 'github'].includes(src.expression.name), `${filename}: non-branding image`);
    }
  }
  for (const feature of loadProductFeatures()) {
    assert.equal(feature.imageKey, undefined, `${feature.slug}: screenshot data remains`);
    assert.equal(feature.imageAlt, undefined, `${feature.slug}: screenshot data remains`);
    assert.ok(feature.demoItems?.length, `${feature.slug}: missing text overview`);
  }
});

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
  for (const feature of loadProductFeatures()) {
    assert.ok(slugs.has(feature.docsPath.split('/')[2]), `${feature.slug}: unknown guide`);
  }
});
