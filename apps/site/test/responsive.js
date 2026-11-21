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
      version: '0.4.0', platforms: {macos: {name: 'download.dmg', url: `${origin}/download.dmg`, size: 12_000_000}},
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

      await context.test('touch screens avoid sticky hover and search input zoom', async () => {
        for (const route of routes) {
          await page.goto(origin + route.path);
          const ungated = await page.evaluate(() => [...document.styleSheets].flatMap((sheet) => [...sheet.cssRules])
            .filter((rule) => rule.selectorText?.includes(':hover')).map((rule) => rule.selectorText));
          assert.deepEqual(ungated, [], route.path);
        }
        await page.goto(origin + '/docs/');
        const size = await page.getByRole('searchbox', {name: 'Search documentation'}).evaluate((element) => ({
          coarse: matchMedia('(pointer: coarse)').matches, font: getComputedStyle(element).fontSize,
        }));
        if (size.coarse) assert.equal(size.font, '16px');
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

      await context.test('app logo groups and capability links stay visible after folding and resizing', async () => {
        await page.setViewportSize(viewports[3]);
        await page.goto(origin);
        await page.getByRole('link', {name: 'Explore'}).click();
        assert.equal(new URL(page.url()).hash, '#explore');
        for (const viewport of viewports) {
          await page.setViewportSize(viewport);
          for (const group of await page.locator('#explore .connection-group').all()) {
            await group.scrollIntoViewIfNeeded();
            await contained(group, 'App group');
            assert.equal(await group.getByRole('listitem').count(), 3);
          }
          await noOverflow(page, 'Apps and capabilities');
        }
        await page.getByRole('link', {name: 'See the Permissions plugin'}).click();
        await page.waitForURL('**/plugins/permissions/');
      });

      assert.deepEqual(errors, [], 'Browser runtime errors');
    } finally {
      await browser.close();
    }
  });
}

test('installation tabs default to the Nightly desktop download and can copy the CLI command', async () => {
  const browser = await chromium.launch();
  try {
    const stable = {version: '0.4.0', platforms: {macos: {name: 'stable.dmg', url: `${origin}/stable.dmg`, size: 12_000_000}}};
    const nightly = {version: '0.4.0', build: '4000.13.1', macos: {name: 'nightly.dmg', url: `${origin}/nightly.dmg`, size: 57_000_000}};
    const zip = {...nightly, macos: {...nightly.macos, name: 'nightly.zip', url: `${origin}/nightly.zip`}};
    for (const [hasStable, preview] of [[true, nightly], [true, null], [false, zip], [false, nightly], [false, null]]) {
      const page = await browser.newPage({viewport: {width: 320, height: 568}, permissions: ['clipboard-read', 'clipboard-write']});
      await page.route('**/api/downloads*', (route) => hasStable ? route.fulfill({json: stable}) : route.fulfill({status: 503, json: {}}));
      await page.route('**/api/nightly*', (route) => preview ? route.fulfill({json: preview}) : route.fulfill({status: 503, json: {}}));
      const checkedStable = page.waitForResponse('**/api/downloads*');
      await page.goto(origin);
      await checkedStable;
      const want = preview?.macos.url ?? (hasStable ? stable.platforms.macos.url : '#download');
      await page.locator(`.hero a.primary[href="${want}"]`).waitFor();
      const desktop = page.getByRole('tab', {name: 'Desktop'});
      assert.equal(await desktop.getAttribute('aria-selected'), 'true');
      assert.equal(await page.getByRole('tabpanel', {name: 'Desktop'}).count(), 1);
      assert.deepEqual(await page.locator('.download-line a').evaluateAll((links) => links.map((link) => link.href)),
        want === '#download' ? [] : [want]);
      assert.equal(await page.getByRole('link', {name: 'Download Nightly'}).count(), 0);
      if (want !== '#download') {
        assert.match(await page.locator('.download-line p').textContent(), /^Version 0\.4\.0/);
        assert.ok(await page.locator('.download-line').evaluate((element) =>
          element.querySelector('p').getBoundingClientRect().top >= element.querySelector('a').getBoundingClientRect().bottom));
      }
      if (preview === zip) assert.match(await page.locator('#download').textContent(), /Unzip the download/);
      await desktop.press('ArrowRight');
      const phone = page.getByRole('tab', {name: 'Phone'});
      assert.equal(await phone.getAttribute('aria-selected'), 'true');
      assert.ok(await page.getByRole('button', {name: 'Install TestFlight beta'}).isDisabled());
      await phone.press('ArrowRight');
      assert.equal(await page.getByRole('tab', {name: 'Terminal'}).getAttribute('aria-selected'), 'true');
      await page.getByRole('button', {name: 'Copy Terminal install command'}).click();
      await page.locator('.terminal-status').getByText('Copied', {exact: true}).waitFor();
      assert.equal(await page.evaluate(() => navigator.clipboard.readText()), 'curl -fsSL https://polymux.com/install-cli.sh | sh');
      await noOverflow(page, 'Installation and command');
      await page.close();
    }
  } finally {
    await browser.close();
  }
});
