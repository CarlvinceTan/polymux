import assert from 'node:assert/strict';
import {resolve} from 'node:path';
import {before, after, afterEach, test} from 'node:test';
import {build} from 'vite';
import {svelte} from '@sveltejs/vite-plugin-svelte';
import {JSDOM} from 'jsdom';
import {loadContent, siteRoot} from '../scripts/content.ts';
import {createRoutes} from '../scripts/routes.ts';
import {browserGlobals} from './browser.js';

const routes = createRoutes(loadContent());
let page;
let restore;
let runtime;
let instance;
before(async () => {
  const entry = resolve(siteRoot, 'navigation-test.js');
  const bundle = await build({root: siteRoot, configFile: false, logLevel: 'silent', plugins: [svelte(), {
    name: 'navigation-test',
    resolveId(id) {if (id === entry) return '\0' + entry;},
    load(id) {
      if (id !== '\0' + entry) return;
      return `import Docs from ${JSON.stringify(resolve(siteRoot, 'src/pages/docs/DocsPage.svelte'))};
        import Menu from ${JSON.stringify(resolve(siteRoot, 'src/components/MobileMenu.svelte'))};
        import Releases from ${JSON.stringify(resolve(siteRoot, 'src/pages/releases/Releases.svelte'))};
        import {mount, unmount, flushSync} from 'svelte';
        import ${JSON.stringify(resolve(siteRoot, 'src/styles/tokens.css'))};
        import ${JSON.stringify(resolve(siteRoot, 'src/styles/base.css'))};
        import ${JSON.stringify(resolve(siteRoot, 'src/styles/layout.css'))};
        import ${JSON.stringify(resolve(siteRoot, 'src/pages/docs/styles.css'))};
        export {unmount, flushSync};
        export function render(name, target, props) {return mount({Docs, Menu, Releases}[name], {target, props});}`;
    },
  }], build: {write: false, minify: false, lib: {entry, formats: ['es'], fileName: 'navigation'}}});
  page = new JSDOM('', {url: 'https://polymux.com/docs/', pretendToBeVisual: true});
  restore = browserGlobals(page.window);
  const output = (Array.isArray(bundle) ? bundle[0] : bundle).output;
  const style = page.window.document.createElement('style');
  style.textContent = output.filter((file) => file.type === 'asset' && file.fileName.endsWith('.css')).map((file) => file.source).join('\n');
  page.window.document.head.append(style);
  const code = output.find((file) => file.type === 'chunk' && file.isEntry).code;
  runtime = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
});
afterEach(async () => {if (instance) await runtime.unmount(instance); instance = undefined; page?.window.document.body.replaceChildren();});
after(() => restore?.());

function render(name, props) {
  instance = runtime.render(name, page.window.document.body, props);
  runtime.flushSync();
  return page.window.document;
}

test('mobile navigation stays inert when closed and Escape restores the toggle focus', () => {
  const document = render('Menu', {active: 'docs'});
  const button = document.querySelector('.mobile-menu-toggle');
  const drawer = document.querySelector('.mobile-menu-drawer');
  assert.equal(button.getAttribute('aria-expanded'), 'false');
  assert.ok(drawer.inert || drawer.hasAttribute('inert'));
  runtime.flushSync(() => button.click());
  assert.equal(button.getAttribute('aria-expanded'), 'true');
  drawer.querySelector('a').focus();
  runtime.flushSync(() => page.window.dispatchEvent(new page.window.KeyboardEvent('keydown', {key: 'Escape'})));
  assert.equal(document.activeElement, button);
  assert.equal(button.getAttribute('aria-expanded'), 'false');
  assert.ok(drawer.inert || drawer.hasAttribute('inert'));
});

test('documentation navigation opens a closed native dialog and restores focus on close', () => {
  const document = render('Docs', routes.find((route) => route.path === '/docs/').props);
  const button = document.querySelector('.docs-menu-button');
  const dialog = document.querySelector('.docs-drawer');
  assert.equal(dialog.open, false);
  runtime.flushSync(() => button.click());
  assert.equal(dialog.open, true);
  assert.equal(dialog.querySelectorAll('nav a').length, 5);
  runtime.flushSync(() => dialog.querySelector('header button').click());
  assert.equal(dialog.open, false);
  assert.equal(document.activeElement, button);
});

test('documentation search uses the prepared search index and keyboard shortcut', () => {
  const document = render('Docs', routes.find((route) => route.path === '/docs/').props);
  const input = document.querySelector('input[type="search"]');
  runtime.flushSync(() => page.window.dispatchEvent(new page.window.KeyboardEvent('keydown', {key: 'k', ctrlKey: true, cancelable: true})));
  assert.equal(document.activeElement, input);
  runtime.flushSync(() => {input.value = 'memory'; input.dispatchEvent(new page.window.Event('input', {bubbles: true}));});
  assert.ok(document.querySelector('.docs-search-results a[href="/docs/workspace/"]'));
  runtime.flushSync(() => page.window.dispatchEvent(new page.window.KeyboardEvent('keydown', {key: 'Escape'})));
  assert.equal(input.value, '');
  assert.equal(document.querySelector('.docs-search-results'), null);
});

test('release filtering keeps arrow-key focus and matching accessible panels without fetching retired downloads', (context) => {
  const fetch = context.mock.method(globalThis, 'fetch', () => {throw new Error('Historical releases must not fetch downloads.');});
  const document = render('Releases', routes.find((route) => route.kind === 'releases' && route.props.release.historical).props);
  const tabs = [...document.querySelectorAll('[role="tab"]')];
  tabs[0].focus();
  runtime.flushSync(() => tabs[0].dispatchEvent(new page.window.KeyboardEvent('keydown', {key: 'ArrowRight', bubbles: true, cancelable: true})));
  assert.equal(document.activeElement, tabs[1]);
  assert.equal(tabs[1].getAttribute('aria-selected'), 'true');
  const visible = document.querySelectorAll('[role="tabpanel"]:not([hidden])');
  assert.equal(visible.length, 1);
  assert.equal(visible[0].id, tabs[1].getAttribute('aria-controls'));
  assert.match(document.querySelector('.historical-note').textContent, /retired/);
  assert.equal(fetch.mock.callCount(), 0);
});
