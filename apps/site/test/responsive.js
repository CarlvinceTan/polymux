import assert from 'node:assert/strict';
import {mkdtempSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {after, before, test} from 'node:test';
import {build, preview} from 'vite';
import {chromium, webkit} from 'playwright';
import {loadContent, siteRoot} from '../scripts/content.ts';
import {createRoutes} from '../scripts/routes.ts';

// Safari's usable viewports: https://webkit.org/blog/18486/webkit-features-for-safari-27-1/
const viewports = [
  {width: 382, height: 580}, // Folded portrait.
  {width: 594, height: 368}, // Folded landscape.
  {width: 669, height: 867}, // Unfolded portrait.
  {width: 951, height: 585}, // Unfolded landscape.
  {width: 385, height: 571}, // Split View.
  {width: 320, height: 568},
  {width: 1280, height: 800},
];
const routes = createRoutes(loadContent());
let output;
let server;
let origin;

before(async () => {
  output = mkdtempSync(join(tmpdir(), 'polymux-responsive-'));
  await build({root: siteRoot, logLevel: 'silent', build: {outDir: output, emptyOutDir: true}});
  server = await preview({root: siteRoot, logLevel: 'silent', build: {outDir: output}, preview: {host: '127.0.0.1', port: 0}});
  origin = `http://127.0.0.1:${server.httpServer.address().port}`;
});

after(async () => {
  if (server) await new Promise((resolve) => server.httpServer.close(resolve));
  if (output) rmSync(output, {recursive: true});
});

async function contained(locator, label) {
  const bounds = await locator.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return {left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom, width: innerWidth, height: innerHeight};
  });
  assert.ok(bounds.left >= -1 && bounds.right <= bounds.width + 1 && bounds.top >= -1 && bounds.bottom <= bounds.height + 1,
    `${label}: ${JSON.stringify(bounds)}`);
}

async function noOverflow(page, label) {
  const size = await page.evaluate(() => ({viewport: innerWidth, content: document.documentElement.scrollWidth}));
  assert.ok(size.content <= size.viewport + 1, `${label}: ${JSON.stringify(size)}`);
}

for (const engine of [webkit, chromium]) {
  test(`${engine.name()}: Duo layouts and live resizing`, async (context) => {
    const browser = await engine.launch();
    const session = await browser.newContext({viewport: viewports[0], isMobile: true, hasTouch: true, deviceScaleFactor: 3});
    const page = await session.newPage();
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await page.route('**/api/downloads*', (route) => route.fulfill({json: {
      version: '0.4.0', platforms: {macos: {url: `${origin}/download.dmg`, size: 12_000_000}},
    }}));
    try {
      await context.test('every published page fits while folding, rotating, and returning to desktop', async () => {
        for (const route of routes) {
          await page.goto(origin + route.path);
          await page.locator('main h1').waitFor();
          await page.evaluate(() => document.fonts.ready);
          for (const viewport of viewports) {
            await page.setViewportSize(viewport);
            await noOverflow(page, `${route.path} at ${viewport.width}px`);
          }
        }
      });

      await context.test('navigation stays reachable on short screens and closes across the desktop breakpoint', async () => {
        await page.goto(origin);
        for (const viewport of viewports.slice(0, 5)) {
          await page.setViewportSize(viewport);
          await page.getByRole('button', {name: 'Open navigation menu'}).click();
          const drawer = page.locator('.mobile-menu-drawer');
          await contained(drawer, 'Site navigation');
          const last = drawer.getByRole('link', {name: 'GitHub', exact: true});
          await last.scrollIntoViewIfNeeded();
          await contained(last, 'Last navigation link');
          await page.getByRole('button', {name: 'Close navigation menu'}).click();
        }
        await page.getByRole('button', {name: 'Open navigation menu'}).click();
        await page.setViewportSize(viewports.at(-1));
        await page.waitForFunction(() => document.querySelector('.mobile-menu-toggle').getAttribute('aria-expanded') === 'false');
        await page.setViewportSize(viewports[0]);
        assert.equal(await page.locator('.mobile-menu-toggle').getAttribute('aria-expanded'), 'false');
        assert.equal(await page.locator('.mobile-menu-drawer').evaluate((element) => element.inert), true);
      });

      await context.test('documentation results and navigation remain inside the folded landscape viewport', async () => {
        await page.setViewportSize(viewports[1]);
        await page.goto(origin + '/docs/');
        const search = page.getByRole('searchbox', {name: 'Search documentation'});
        await search.fill('the');
        const results = page.getByRole('navigation', {name: 'Search results'});
        assert.ok(await results.getByRole('link').count() > 1);
        await contained(page.locator('.docs-search-results'), 'Search results');
        assert.ok(await search.evaluate((element) => parseFloat(getComputedStyle(element).fontSize)) >= 16);
        const last = results.getByRole('link').last();
        await last.scrollIntoViewIfNeeded();
        await contained(last, 'Last search result');
        await last.click();
        await page.waitForURL((url) => url.pathname !== '/docs/');
        await page.getByRole('button', {name: 'Open documentation navigation'}).click();
        const drawer = page.getByRole('dialog', {name: 'Documentation navigation'});
        await contained(drawer, 'Documentation navigation');
        await drawer.getByRole('button', {name: 'Close', exact: true}).click();
      });

      await context.test('demo selection, native-size zoom, and focus survive folding without reloading', async () => {
        await page.setViewportSize(viewports[3]);
        await page.goto(origin);
        await page.getByRole('button', {name: 'Nova', exact: true}).click();
        const zoom = page.getByRole('button', {name: 'Zoom', exact: true});
        await zoom.click();
        const dialog = page.getByRole('dialog', {name: 'Native app preview', exact: true});
        for (const viewport of viewports.slice(0, 5)) {
          await page.setViewportSize(viewport);
          await contained(dialog, 'Zoom dialog');
          await contained(dialog.getByRole('button', {name: 'Close', exact: true}), 'Zoom close button');
          assert.equal(await dialog.locator('img:not([hidden])').getAttribute('data-native-capture'), 'nova');
          const scroll = await dialog.locator('.zoom-viewport').evaluate((element) => {
            element.scrollLeft = element.scrollWidth;
            return {left: element.scrollLeft, width: element.clientWidth, content: element.scrollWidth};
          });
          assert.ok(scroll.content <= scroll.width || scroll.left > 0, 'Native capture must pan inside Zoom.');
          await noOverflow(page, 'Page with Zoom open');
        }
        await dialog.getByRole('button', {name: 'Close', exact: true}).click();
        assert.equal(await zoom.evaluate((element) => document.activeElement === element), true);
        assert.equal(await page.getByRole('button', {name: 'Nova', exact: true}).getAttribute('aria-pressed'), 'true');
        await page.getByText('Transcript', {exact: true}).click();
        assert.equal(await page.locator('.transcript').getAttribute('open'), '');
      });

      assert.deepEqual(errors, [], 'Browser runtime errors');
    } finally {
      await browser.close();
    }
  });
}
