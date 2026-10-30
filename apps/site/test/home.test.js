import assert from 'node:assert/strict';
import {readFileSync, readdirSync} from 'node:fs';
import {resolve} from 'node:path';
import {before, after, test} from 'node:test';
import {JSDOM} from 'jsdom';
import {loadContent, siteRoot, checkGuide} from '../scripts/content.ts';
import {createRoutes} from '../scripts/routes.ts';
import {renderer} from './helpers.js';

const content = loadContent();
const routes = createRoutes(content);
let server;
let home;
before(async () => {
  server = await renderer();
  home = new JSDOM(await server.render(routes.find((route) => route.path === '/')));
});
after(async () => {home?.window.close(); await server?.close();});

test('installation distinguishes the Mac server and client without offering legacy downloads', () => {
  const document = home.window.document;
  const installation = document.querySelector('#download');
  assert.match(installation.textContent, /Native downloads aren’t published yet/);
  assert.match(installation.textContent, /source checkout/);
  const cards = [...installation.querySelectorAll('article')];
  assert.deepEqual(cards.map((card) => card.querySelector('h3').textContent), ['Server', 'Client']);
  assert.deepEqual(cards.map((card) => card.querySelector('code').textContent), ['./scripts/install.sh server', './scripts/install.sh app']);
  assert.deepEqual(cards.map((card) => card.querySelector('a').getAttribute('href')), ['/docs/installation/#server', '/docs/installation/#client']);
  assert.equal(document.querySelectorAll('a[href*="releases/download"], a[href="/install.sh"]').length, 0);
  assert.equal(document.querySelector('.mobile-menu-download').textContent.trim(), 'Install');
  assert.match(content.docs.find((page) => page.slug === 'installation').body, /git clone --branch dev https:\/\/github\.com\/CarlvinceTan\/polymux\.git/);
});

test('home links resolve to existing sections and guide headings', () => {
  const document = home.window.document;
  for (const link of document.querySelectorAll('a[href^="#"]')) assert.ok(document.getElementById(link.getAttribute('href').slice(1)));
  for (const link of document.querySelectorAll('a[href^="/docs/"]')) checkGuide(link.getAttribute('href'), content.docs, 'home');
});

test('every documentation route prerenders its article and consistent navigation without globals', async () => {
  for (const route of routes.filter((route) => route.kind === 'docs')) {
    const dom = new JSDOM(await server.render(route));
    try {
      const document = dom.window.document;
      assert.equal(document.querySelector('.docs-article h1').textContent, route.props.page.title);
      const links = [...document.querySelectorAll('.docs-sidebar a')];
      assert.equal(links.length, content.docs.length);
      assert.equal(links.filter((link) => link.getAttribute('aria-current') === 'page').length, 1);
      assert.equal(document.querySelector('.install-link').getAttribute('href'), '/docs/installation/');
      assert.ok(document.querySelector('dialog.docs-drawer:not([open])'));
      assert.ok(document.querySelector('label[for="docs-search"]'));
    } finally {dom.window.close();}
  }
});

test('all pages share native branding, installation, skip links, and meaningful rendered content', async () => {
  for (const route of routes) {
    const dom = new JSDOM(await server.render(route));
    try {
      const document = dom.window.document;
      assert.match(document.querySelector('.site-brand img').getAttribute('src'), /app-icon\.webp/);
      assert.equal(document.querySelectorAll('header.site-header').length, 1);
      assert.equal(document.querySelectorAll('footer.site-footer').length, 1);
      assert.ok(document.querySelector('main#main'));
      assert.ok(document.querySelector('a.skip-link[href="#main"]'));
      assert.ok(document.querySelector('main').textContent.trim().length > 50, route.path);
      assert.equal(document.querySelector('.install-link').textContent.trim(), 'Install');
      for (const link of document.querySelectorAll('a[href^="/docs/"]')) checkGuide(link.getAttribute('href'), content.docs, route.path);
    } finally {dom.window.close();}
  }
});

test('recursive source checks cover nested pages and components rather than silently skipping moved files', () => {
  const files = readdirSync(resolve(siteRoot, 'src'), {recursive: true}).filter((file) => /\.(svelte|ts)$/.test(file));
  assert.ok(files.includes('pages/docs/DocsPage.svelte'));
  assert.ok(files.includes('components/AgentDemo.svelte'));
  for (const filename of files) {
    const source = readFileSync(resolve(siteRoot, 'src', filename), 'utf8');
    assert.ok(!source.includes('assets/polymux.svg'), `${filename}: old branding`);
    assert.doesNotMatch(source, /import\.meta\.glob|from ['"](?:yaml|marked|dompurify)['"]/, `${filename}: content parsing belongs in the build`);
  }
  const bootstrap = readFileSync(resolve(siteRoot, 'src/lib/page.ts'), 'utf8');
  for (const style of ['tokens', 'base', 'layout', 'prose']) assert.ok(bootstrap.includes(`../styles/${style}.css`));
});

test('product preview and copy explain where agents run without recreating native UI', () => {
  const document = home.window.document;
  assert.match(document.querySelector('.app-preview img').getAttribute('src'), /polymux-macos\.webp/);
  const setup = document.querySelector('#how-it-works');
  assert.equal(setup.querySelectorAll('figure, svg').length, 0);
  assert.match(setup.textContent, /server runs your agents/i);
  assert.match(setup.textContent, /same Mac/);
});
