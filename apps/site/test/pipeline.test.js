import assert from 'node:assert/strict';
import {mkdtempSync, readFileSync, rmSync, symlinkSync, existsSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {before, after, test} from 'node:test';
import {build, createServer, preview} from 'vite';
import {svelte} from '@sveltejs/vite-plugin-svelte';
import {JSDOM} from 'jsdom';
import {loadContent, siteRoot} from '../scripts/content.ts';
import {createRoutes} from '../scripts/routes.ts';
import {pagesRoot, prepare} from '../scripts/prepare.ts';
import {assetLinks, previewPlugin, sitePlugin} from '../scripts/vite.ts';
import {fixtureRoot} from './helpers.js';
import {browserGlobals} from './browser.js';

const content = loadContent();
const routes = createRoutes(content);
let output;
let built;
let server;
let origin;
before(async () => {
  output = mkdtempSync(join(tmpdir(), 'polymux-site-build-'));
  built = await build({root: siteRoot, logLevel: 'silent', build: {outDir: output, emptyOutDir: true}});
  server = await preview({root: siteRoot, logLevel: 'silent', build: {outDir: output}, preview: {host: '127.0.0.1', port: 0}});
  origin = `http://127.0.0.1:${server.httpServer.address().port}`;
});
after(async () => {
  if (server) await new Promise((resolve) => server.httpServer.close(resolve));
  if (output) rmSync(output, {recursive: true});
});

test('the production server serves prerendered content, metadata, styles, and resolved assets on every route', async () => {
  const assets = new Set();
  for (const route of routes) {
    const response = await fetch(`${origin}${route.path}`);
    assert.equal(response.status, 200, route.path);
    const source = await response.text();
    assert.ok(!/<!--site-head-->|(?:src|href)="\/src\//.test(source), `${route.path}: unresolved source attribute`);
    const dom = new JSDOM(source);
    try {
      const document = dom.window.document;
      assert.ok(document.querySelector('main h1'), route.path);
      assert.equal(document.querySelector('title').textContent, route.title);
      assert.equal(document.querySelectorAll('title').length, 1);
      assert.equal(document.querySelector('meta[name="description"]').content, route.description);
      assert.equal(document.querySelector('link[rel="canonical"]').href, `https://polymux.com${route.path}`);
      assert.ok(document.querySelector('link[rel="stylesheet"]'), route.path);
      assert.deepEqual(JSON.parse(document.querySelector('#page-data').textContent), JSON.parse(JSON.stringify(route.props)));
      const schema = JSON.parse(document.querySelector('script[type="application/ld+json"]').textContent);
      if (route.kind === 'home') assert.equal(schema['@graph'].find((item) => item['@type'] === 'SoftwareApplication').downloadUrl, 'https://polymux.com/docs/installation/');
      if (route.kind === 'post') assert.equal(schema.headline, route.props.post.title);
      assert.ok(document.querySelector('main').textContent.trim().length > 50);
      for (const element of document.querySelectorAll('[src^="/assets/"], [href^="/assets/"]')) assets.add(element.getAttribute('src') ?? element.getAttribute('href'));
    } finally {dom.window.close();}
  }
  for (const asset of assets) assert.equal((await fetch(origin + asset)).status, 200, asset);
});

test('production browser bundles contain no Markdown parser, YAML parser, sanitizer, or raw content modules', () => {
  const modules = (Array.isArray(built) ? built : [built]).flatMap((result) => result.output).filter((item) => item.type === 'chunk').flatMap((chunk) => Object.keys(chunk.modules));
  assert.ok(modules.some((path) => path.includes('/src/components/AgentDemo.svelte')));
  assert.ok(!modules.some((path) => /node_modules\/(?:yaml|marked|dompurify)\//.test(path)));
  assert.ok(!modules.some((path) => /content\/.*\.md/.test(path)));
});

test('machine-readable documentation is emitted from the same published guide bodies and links', () => {
  const overview = readFileSync(resolve(output, 'llms.txt'), 'utf8');
  const full = readFileSync(resolve(output, 'llms-full.txt'), 'utf8');
  for (const guide of content.docs) {
    assert.ok(overview.includes(guide.description));
    assert.ok(full.includes(guide.body));
    const path = guide.slug === 'introduction' ? '/docs/' : `/docs/${guide.slug}/`;
    assert.ok(overview.includes(`https://polymux.com${path}`));
  }
  assert.ok(!existsSync(resolve(siteRoot, 'public/llms-full.txt')));
});

test('draft YAML variants never enter emitted HTML, machine docs, or browser bundles', async () => {
  const fixture = fixtureRoot();
  try {
    for (const [index, published] of ['false # pending', 'False', '!!bool false'].entries()) {
      fixture.write(`src/content/blog/draft-${index}.md`, `---\ntitle: PRIVATE_DRAFT_SENTINEL\nslug: draft-${index}\ndate: 2026-10-24\nexcerpt: PRIVATE_DRAFT_SENTINEL\npublished: ${published}\n---\nPRIVATE_DRAFT_SENTINEL\n`);
    }
    fixture.write('src/content/blog/public.md', '---\ntitle: PUBLIC_POST_SENTINEL\nslug: public\ndate: 2026-10-24\nexcerpt: A published article\npublished: true\n---\n# Public article\nPublished text.');
    const prepared = prepare({root: fixture.root});
    const result = await build({root: siteRoot, configFile: false, logLevel: 'silent', publicDir: false,
      plugins: [sitePlugin(prepared), svelte(), assetLinks()],
      build: {write: false, rolldownOptions: {input: resolve(pagesRoot(), 'blog/index.html')}},
    });
    const emitted = (Array.isArray(result) ? result : [result]).flatMap((result) => result.output)
      .map((item) => item.type === 'chunk' ? item.code : String(item.source)).join('\n');
    assert.ok(emitted.includes('PUBLIC_POST_SENTINEL'));
    assert.ok(!emitted.includes('PRIVATE_DRAFT_SENTINEL'));
  } finally {rmSync(fixture.root, {recursive: true});}
});

test('the actual production entry hydrates prerendered markup without mismatches and retains demo interactions', async (context) => {
  const html = await (await fetch(origin)).text();
  const page = new JSDOM(html, {url: origin, pretendToBeVisual: true});
  const restore = browserGlobals(page.window);
  const warnings = [];
  context.mock.method(console, 'warn', (...messages) => warnings.push(messages.join(' ')));
  try {
    const script = page.window.document.querySelector('script[type="module"]').getAttribute('src');
    await import(pathToFileURL(resolve(output, script.slice(1))).href);
    await new Promise(setImmediate);
    const document = page.window.document;
    assert.equal(document.querySelectorAll('header.site-header').length, 1);
    assert.equal(document.querySelectorAll('.agent-demo').length, 1);
    document.querySelectorAll('.agent-choices button')[1].click();
    await new Promise(setImmediate);
    assert.equal(document.querySelector('.native-preview img:not([hidden])').getAttribute('data-native-capture'), 'nova');
    assert.ok(!warnings.some((message) => message.includes('hydration')), warnings.join('\n'));
  } finally {restore();}
});

test('production routing retires old distribution, preserves documentation redirects, and returns genuine 404s', async () => {
  for (const path of ['/install.sh', '/api/releases', '/api/updates/win32/x64/RELEASES', '/api/updates/linux/x64/latest-linux.yml']) {
    const response = await fetch(origin + path);
    assert.equal(response.status, 410, path);
    assert.match(await response.text(), /retired/);
  }
  for (const path of ['/admin', '/admin/', '/admin/index.html', '/admin/config.yml', '/share/old-chat', '/docs/not-a-guide/', '/product/not-a-feature/', '/blog/not-a-post/']) assert.equal((await fetch(origin + path)).status, 404, path);
  const redirect = await fetch(`${origin}/docs/memory/`, {redirect: 'manual'});
  assert.equal(redirect.status, 308);
  assert.equal(redirect.headers.get('location'), '/docs/workspace/#memory');
  for (const path of ['/privacy', '/privacy/', '/privacy/index.html']) {
    const privacy = await fetch(origin + path, {redirect: 'manual'});
    assert.equal(privacy.status, 308, path);
    assert.equal(privacy.headers.get('location'), '/privacy-policy/');
  }
  const sitemap = await fetch(`${origin}/sitemap.xml`);
  assert.equal(sitemap.status, 200);
  assert.match(sitemap.headers.get('content-type'), /xml/);
  const xml = await sitemap.text();
  for (const route of routes) assert.ok(xml.includes(`https://polymux.com${route.path}</loc>`), route.path);
  assert.ok(!xml.includes('https://polymux.com/privacy/'));
});

test('development watches content creation, edits, and removal without restarting', async () => {
  const fixture = fixtureRoot();
  let dev;
  try {
    for (const directory of ['pages', 'components', 'assets', 'lib', 'styles']) symlinkSync(resolve(siteRoot, 'src', directory), resolve(fixture.root, 'src', directory), 'dir');
    symlinkSync(resolve(siteRoot, 'node_modules'), resolve(fixture.root, 'node_modules'), 'dir');
    const prepared = prepare({root: fixture.root});
    dev = await createServer({root: fixture.root, cacheDir: resolve(fixture.root, '.vite'), configFile: false, logLevel: 'silent', appType: 'mpa',
      plugins: [sitePlugin(prepared, fixture.root), svelte()],
      // Polls, as the site's config does: macOS file events can arrive after this test's wait.
      server: {host: '127.0.0.1', port: 0, hmr: false, ws: false, watch: {usePolling: true, interval: 25}, fs: {allow: [fixture.root, siteRoot]}},
    });
    await dev.listen();
    const devOrigin = `http://127.0.0.1:${dev.httpServer.address().port}`;
    const privacy = await fetch(`${devOrigin}/privacy/`, {redirect: 'manual'});
    assert.equal(privacy.status, 308);
    assert.equal(privacy.headers.get('location'), '/privacy-policy/');
    assert.match(await (await fetch(`${devOrigin}/sitemap.xml`)).text(), /privacy-policy/);
    const home = new JSDOM(await (await fetch(devOrigin)).text());
    try {
      for (const element of home.window.document.querySelectorAll('script[type="module"][src], img[src]')) {
        const asset = element.getAttribute('src');
        assert.equal((await fetch(new URL(asset, devOrigin))).status, 200, asset);
      }
    } finally {home.window.close();}
    await new Promise((resolve) => {
      if (Object.keys(dev.watcher.getWatched()).some((directory) => directory.endsWith('/src/content/blog'))) resolve();
      else dev.watcher.once('ready', resolve);
    });
    const url = `http://127.0.0.1:${dev.httpServer.address().port}/blog/new-post/`;
    const waitFor = async (predicate) => {
      const deadline = Date.now() + 5000;
      while (Date.now() < deadline) {
        if (await predicate()) return;
        await new Promise((resolve) => setTimeout(resolve, 25));
      }
      assert.fail('Development content did not refresh.');
    };
    const post = (title) => `---\ntitle: ${title}\nslug: new-post\ndate: 2026-10-24\nexcerpt: A fresh article\npublished: true\n---\n# A new post\n\nArticle text.\n`;
    fixture.write('src/content/blog/new-post.md', post('First title'));
    await waitFor(async () => (await fetch(url)).status === 200);
    assert.match(await (await fetch(url)).text(), /First title/);
    fixture.write('src/content/blog/new-post.md', post('Updated title'));
    await waitFor(async () => (await (await fetch(url)).text()).includes('Updated title'));
    rmSync(resolve(fixture.root, 'src/content/blog/new-post.md'));
    await waitFor(async () => (await fetch(url)).status === 404);
    assert.ok(!existsSync(resolve(pagesRoot(fixture.root), 'blog/new-post/index.html')));
  } finally {
    await dev?.close();
    rmSync(fixture.root, {recursive: true});
  }
});

test('preview serves the built snapshot even when source content has changed or become invalid', async () => {
  const fixture = fixtureRoot();
  let previewServer;
  try {
    fixture.write('dist/index.html', '<!doctype html><h1>Built snapshot</h1>');
    fixture.write('dist/llms-full.txt', 'Published documentation snapshot');
    fixture.write('dist/_site/manifest.json', JSON.stringify({hosting: {}, releases: []}));
    fixture.write('src/content/site.json', 'Invalid work in progress');
    previewServer = await preview({root: fixture.root, configFile: false, plugins: [previewPlugin()], logLevel: 'silent', build: {outDir: 'dist'}, preview: {host: '127.0.0.1', port: 0}});
    const url = `http://127.0.0.1:${previewServer.httpServer.address().port}`;
    assert.equal(await (await fetch(`${url}/llms-full.txt`)).text(), 'Published documentation snapshot');
    const download = await fetch(`${url}/api/downloads`);
    assert.equal(download.status, 503);
    assert.match((await download.json()).error, /not published/);
    assert.ok(!existsSync(resolve(fixture.root, '.generated/releases.json')));
  } finally {
    if (previewServer) await new Promise((resolve) => previewServer.httpServer.close(resolve));
    rmSync(fixture.root, {recursive: true});
  }
});
