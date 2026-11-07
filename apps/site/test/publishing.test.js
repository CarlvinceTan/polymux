import assert from 'node:assert/strict';
import {readFileSync, rmSync, mkdirSync, symlinkSync, existsSync} from 'node:fs';
import {resolve} from 'node:path';
import test from 'node:test';
import {spawnSync} from 'node:child_process';
import {readDocument, date} from '../lib/content/documents.ts';
import {readRelease} from '../lib/content/releases.ts';
import {loadContent} from '../scripts/content.ts';
import {pagesRoot, prepare, outputPath} from '../scripts/prepare.ts';
import {pageHead} from '../scripts/routes.ts';
import {fixtureRoot} from './helpers.js';

test('all YAML false spellings exclude drafts from routes and release metadata', () => {
  const fixture = fixtureRoot();
  try {
    for (const [index, value] of ['false', 'False', '!!bool false', 'false # pending review'].entries()) {
      const source = `---\npublished: ${value}\n---\nPRIVATE_DRAFT_${index}`;
      assert.equal(readDocument(source, 'draft.md').published, false);
      fixture.write(`src/content/blog/draft-${index}.md`, source);
      fixture.write(`src/content/releases/draft-${index}.md`, source);
      fixture.write(`src/content/docs/draft-${index}.md`, source);
    }
    const {content, routes} = prepare({root: fixture.root});
    assert.equal(content.posts.length, 0);
    assert.equal(content.docs.length, 1);
    assert.equal(content.releases.length, 1);
    assert.ok(!JSON.stringify(routes).includes('PRIVATE_DRAFT'));
    assert.ok(!readFileSync(resolve(fixture.root, '.generated/releases.json'), 'utf8').includes('PRIVATE_DRAFT'));
  } finally {rmSync(fixture.root, {recursive: true});}
});

test('content-only release publication prepares its routes and allowlist before dependent checks', () => {
  const fixture = fixtureRoot();
  try {
    prepare({root: fixture.root});
    fixture.write('src/content/releases/v1.1.0.md', '---\nversion: 1.1.0\ntitle: Another release\ndate: 2026-10-25\nsummary: A new native release\npublished: true\ndownloadable: true\n---\n## Features\n### Desktop\n- Another improvement.\n');
    const {routes} = prepare({root: fixture.root});
    assert.ok(routes.some((route) => route.path === '/releases/1.1.0/'));
    assert.equal(JSON.parse(readFileSync(resolve(fixture.root, '.generated/releases.json'), 'utf8'))[0].version, '1.1.0');
  } finally {rmSync(fixture.root, {recursive: true});}
});

test('the Vercel function build prepares metadata independently of frontend generation', () => {
  const fixture = fixtureRoot();
  try {
    fixture.write('src/content/site.json', 'not frontend content');
    const script = new URL('../scripts/api.ts', import.meta.url);
    const result = spawnSync(process.execPath, [script.pathname, fixture.root], {encoding: 'utf8'});
    assert.equal(result.status, 0, result.stderr);
    const releases = JSON.parse(readFileSync(resolve(fixture.root, '.generated/releases.json'), 'utf8'));
    assert.equal(releases[0].version, '1.0.0');
    assert.equal(releases[0].downloadable, true);
    const packageJson = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));
    assert.equal(packageJson.scripts['vercel-build'], 'node scripts/api.ts');
  } finally {rmSync(fixture.root, {recursive: true});}
});

test('invalid slugs fail before cleanup can delete existing generated output', () => {
  const fixture = fixtureRoot();
  try {
    const post = (slug) => `---\ntitle: Post\nslug: ${JSON.stringify(slug)}\ndate: 2026-10-24\nexcerpt: A post\npublished: true\n---\nBody\n`;
    fixture.write('src/content/blog/post.md', post('post'));
    prepare({root: fixture.root});
    const route = resolve(pagesRoot(fixture.root), 'blog/post/index.html');
    const original = readFileSync(route, 'utf8');
    for (const slug of ['', '../docs', 'nested/voice', '.', '/outside']) {
      fixture.write('src/content/blog/post.md', post(slug));
      assert.throws(() => prepare({root: fixture.root}), /slug/);
      assert.equal(readFileSync(route, 'utf8'), original);
    }
    assert.throws(() => outputPath(fixture.root, '../outside/index.html'), /escapes/);
  } finally {rmSync(fixture.root, {recursive: true});}
});

test('generation refuses authored directories and symlink destinations', (context) => {
  const fixture = fixtureRoot();
  try {
    fixture.write('.generated/pages/index.html', 'Authored content');
    assert.throws(() => prepare({root: fixture.root}), /authored pages/);
    assert.equal(readFileSync(resolve(pagesRoot(fixture.root), 'index.html'), 'utf8'), 'Authored content');
    if (process.platform === 'win32') return context.diagnostic('Symlink check is exercised on Unix.');
    rmSync(pagesRoot(fixture.root), {recursive: true});
    mkdirSync(resolve(fixture.root, 'outside'));
    symlinkSync(resolve(fixture.root, 'outside'), pagesRoot(fixture.root), 'dir');
    assert.throws(() => prepare({root: fixture.root}), /symlink/);
  } finally {rmSync(fixture.root, {recursive: true});}
});

test('all page shells stay in generated output and removed routes are cleaned without touching source', () => {
  const fixture = fixtureRoot();
  try {
    fixture.write('notes/keep.txt', 'Authored content');
    fixture.write('src/content/blog/temporary.md', '---\ntitle: Temporary\nslug: temporary\ndate: 2026-10-24\nexcerpt: Temporary article\n---\nArticle text.');
    const {routes, sitemap} = prepare({root: fixture.root});
    for (const route of routes) {
      assert.ok(existsSync(resolve(pagesRoot(fixture.root), route.file)), route.file);
      assert.ok(!existsSync(resolve(fixture.root, route.file)), route.file);
    }
    assert.ok(!existsSync(resolve(fixture.root, 'public/sitemap.xml')));
    assert.match(sitemap, /https:\/\/polymux.com\/privacy-policy\//);
    assert.ok(!sitemap.includes('https://polymux.com/privacy/'));
    rmSync(resolve(fixture.root, 'src/content/blog/temporary.md'));
    prepare({root: fixture.root});
    assert.ok(!existsSync(resolve(pagesRoot(fixture.root), 'blog/temporary/index.html')));
    assert.equal(readFileSync(resolve(fixture.root, 'notes/keep.txt'), 'utf8'), 'Authored content');
  } finally {rmSync(fixture.root, {recursive: true});}
});

test('invalid calendar dates and inconsistent ordering fail at the shared content boundary', () => {
  for (const value of ['2026-99-99', '2026-02-30', '2026-02-29', '2026-1-01']) assert.throws(() => date(value, 'date', 'probe'), /invalid/);
  assert.equal(date('2028-02-29', 'date', 'probe'), '2028-02-29');
  const fixture = fixtureRoot();
  try {
    fixture.write('src/content/docs/introduction.md', '---\ntitle: Start\nslug: introduction\ndescription: Start here\nsection: Guides\norder: 1\n---\n# Start');
    assert.throws(() => loadContent(fixture.root), /sectionOrder/);
  } finally {rmSync(fixture.root, {recursive: true});}
});

test('empty optional release fields are accepted but retired releases cannot re-enable downloads', () => {
  const fields = {version: '1.0.0', title: 'Native release', date: '2026-10-24', summary: 'Native app', tag: '', updated: null};
  const release = readRelease(fields, 'release.md');
  assert.equal(release.tag, 'v1.0.0');
  assert.equal(release.updated, undefined);
  assert.throws(() => readRelease({...fields, historical: true, downloadable: true}, 'release.md'), /distribution is retired/);
  assert.throws(() => readRelease({...fields, tag: false}, 'release.md'), /requires tag/);
});

test('HTML metadata and structured data share explicit modification dates and escape script content', () => {
  const head = pageHead({path: '/blog/probe/', kind: 'post', title: 'A title', description: '</script><script>unsafe</script>', updated: '2026-10-24', props: {post: {date: '2026-01-01', author: 'Author'}, related: []}});
  assert.match(head, /"dateModified":"2026-10-24"/);
  assert.match(head, /\\u003c\/script>/);
  assert.doesNotMatch(head, /<script>unsafe/);
});
