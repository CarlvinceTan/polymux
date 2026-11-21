import assert from 'node:assert/strict';
import {mkdtempSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import test from 'node:test';
import {loadPlugins} from '../lib/content/plugins.ts';
import {loadContent} from '../scripts/content.ts';
import {fetchRegistry} from '../scripts/registry.ts';
import {createRoutes} from '../scripts/routes.ts';
import {fixtureRoot, renderer} from './helpers.js';

function registry(plugins, format = 1) {
  const file = join(mkdtempSync(join(tmpdir(), 'polymux-registry-')), 'index.json');
  writeFileSync(file, JSON.stringify({format, plugins}));
  return file;
}

const release = (version, extra = {}) => ({
  version, publishTimestamp: 1793000000,
  source: {repository: 'example/notes', commit: 'abc', artifacts: {'darwin-arm64': {url: 'u', sha256: 's'}}}, ...extra,
});

test('a plugin shows its newest stable release, as the server recommends', () => {
  const [plugin] = loadPlugins(registry([{name: 'notes', title: 'Notes', kind: 'scoped', homepage: 'https://example.com',
    versions: [release('1.9.0'), release('1.10.0'), release('2.0.0-beta.1'), {version: '3.0.0'}]}]));
  assert.equal(plugin.version, '1.10.0');
  assert.equal(plugin.published, '2026-10-26');
  assert.equal(plugin.source, 'https://github.com/example/notes/tree/abc');
  assert.deepEqual(plugin.targets, ['darwin-arm64']);
});

test('the registry can only link out over https', () => {
  const [plugin] = loadPlugins(registry([{name: 'notes', kind: 'global', homepage: 'javascript:alert(1)',
    versions: [release('1.0.0', {source: {repository: 'not a repo', artifacts: {x: {}}}})]}]));
  assert.equal(plugin.homepage, undefined);
  assert.equal(plugin.repository, undefined);
  assert.equal(plugin.title, 'notes');
});

test('a plugin with no packages is left out, and a bad registry stops the build', () => {
  assert.deepEqual(loadPlugins(registry([{name: 'notes', kind: 'global', versions: [{version: '1.0.0'}]}])), []);
  assert.throws(() => loadPlugins(registry([], 2)), /format 2/);
  assert.throws(() => loadPlugins(registry([{name: '../x', kind: 'global'}])), /invalid/);
  assert.throws(() => loadPlugins(registry([{name: 'x', kind: 'odd'}])), /invalid kind/);
});

test('a build saves the official registry, and a failed fetch stops it', async () => {
  const file = join(mkdtempSync(join(tmpdir(), 'polymux-registry-')), 'nested', 'registry.json');
  const answer = JSON.stringify({format: 1, plugins: [{name: 'notes', kind: 'scoped', versions: [release('1.0.0')]}]});
  await fetchRegistry(file, async () => new Response(answer));
  assert.equal(loadPlugins(file)[0].name, 'notes');
  await assert.rejects(fetchRegistry(file, async () => new Response('', {status: 404})), /answered 404/);
  assert.equal(loadPlugins(file)[0].name, 'notes');
  assert.deepEqual(loadPlugins(join(tmpdir(), 'polymux-no-registry.json')), []);
});

test('each plugin in the registry gets a page, and an empty registry says so', async () => {
  const fixture = fixtureRoot();
  const routes = (plugins) => {
    process.env.POLYMUX_SITE_REGISTRY = registry(plugins);
    return createRoutes(loadContent(fixture.root), fixture.root).filter((route) => route.kind === 'plugins');
  };
  const {render, close} = await renderer();
  try {
    const listed = routes([{name: 'notes', title: 'Notes', kind: 'scoped', description: 'Shared notes', versions: [release('1.0.0')]}]);
    assert.deepEqual(listed.map((route) => route.path), ['/plugins/', '/plugins/notes/']);
    assert.match(await render(listed[0]), /href="\/plugins\/notes\/"[^>]*>Notes/);
    assert.match(await render(listed[1]), /Settings › Plugins › Marketplace › Notes/);
    const [empty] = routes([]);
    assert.match(await render(empty), /No plugins listed\./);
  } finally {
    delete process.env.POLYMUX_SITE_REGISTRY;
    await close();
  }
});
