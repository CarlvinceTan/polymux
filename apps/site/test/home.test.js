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

test('installation offers a guide before the native download resolves', () => {
  const document = home.window.document;
  const installation = document.querySelector('#download');
  assert.equal(installation.querySelector('a[href="/docs/installation/"]').textContent.trim(), 'Installation guide');
  const desktop = installation.querySelector('#install-panel-Desktop');
  assert.doesNotMatch(desktop.textContent, /drag Polymux/);
  const button = desktop.querySelector('button.primary');
  assert.equal(button.textContent.trim(), 'Download for macOS (Beta)');
  assert.ok(button.disabled, 'The Desktop download stays visible but disabled until a real installer resolves.');
  assert.equal(desktop.querySelectorAll('a.primary').length, 0);
  assert.match(desktop.querySelector('.install-note').textContent, /isn’t available yet/);
  assert.equal(document.querySelectorAll('a[href*="releases/download"], a[href="/install.sh"]').length, 0);
  assert.equal(document.querySelector('.mobile-menu-download').textContent.trim(), 'Install');
  const guide = content.docs.find((page) => page.slug === 'installation').body;
  assert.match(guide, /drag Polymux into Applications/);
  assert.match(guide, /start the bundled server/);
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
  assert.ok(files.includes('pages/home/Connections.svelte'));
  for (const filename of files) {
    const source = readFileSync(resolve(siteRoot, 'src', filename), 'utf8');
    assert.ok(!source.includes('assets/polymux.svg'), `${filename}: old branding`);
    assert.doesNotMatch(source, /import\.meta\.glob|from ['"](?:yaml|marked|dompurify)['"]/, `${filename}: content parsing belongs in the build`);
  }
  const bootstrap = readFileSync(resolve(siteRoot, 'src/lib/page.ts'), 'utf8');
  for (const style of ['tokens', 'base', 'layout', 'prose']) assert.ok(bootstrap.includes(`../styles/${style}.css`));
});

test('the hero keeps the voxel island and sample window with nothing supplemental under the window', () => {
  const document = home.window.document;
  const hero = document.querySelector('.hero');
  assert.ok(hero.querySelector('.world[role="img"]'));
  assert.ok(hero.querySelector('.agent-demo .native-preview img[data-native-capture="atlas"]'));
  assert.equal(document.querySelector('.availability, .app-preview, .zoom-button, .capture-dialog, .transcript, .demo-caption, .agent-choices'), null);
  assert.equal(hero.querySelector('.native-preview').nextElementSibling, null);
});

test('the connection map shows wide example breadth and marks Hub groups as an optional plugin', () => {
  const document = home.window.document;
  const section = document.querySelector('#connections');
  const map = section.querySelector('.connection-map');
  assert.match(map.querySelector('.connection-core').textContent, /Polymux/);
  const groups = [...map.querySelectorAll('.connection-group')].map((group) => [group.querySelector('h3').textContent, group.querySelectorAll('li').length]);
  assert.deepEqual(groups, [['Browser', 6], ['Mac apps', 6], ['Messaging via Hub', 8], ['Email via Hub', 4]]);
  assert.equal(map.querySelectorAll('.hub .connection-group').length, 2, 'Only Hub groups sit behind the optional plugin.');
  assert.match(map.querySelector('.connection-plugin').textContent, /Optional Hub plugin/);
  assert.match(section.textContent, /optional Hub plugin/);
  // The service count must match the bridges Hub ships, plus WeChat through its relay.
  const bridges = readFileSync(resolve(siteRoot, '../../plugins/hub/internal/hub/bridges.go'), 'utf8').match(/\{platform: [^,]+, name: "([^"]+)"/g);
  assert.match(map.querySelector('.hub .connection-group header span').textContent, new RegExp(`^${bridges.length + 1} services$`));
  const setup = document.querySelector('#how-it-works');
  assert.match(setup.textContent, /own server.*local model.*recurring work.*optional Permissions plugin/s);
});
