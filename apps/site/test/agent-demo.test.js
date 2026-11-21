import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {after, afterEach, before, test} from 'node:test';
import {svelte} from '@sveltejs/vite-plugin-svelte';
import {JSDOM} from 'jsdom';
import {build} from 'vite';

let page;
let runtime;
let instance;
const globals = new Map();
const root = resolve(import.meta.dirname, '..');
const examples = JSON.parse(readFileSync(resolve(root, 'src/content/demo.json'), 'utf8'));
const manifest = JSON.parse(readFileSync(resolve(root, 'src/assets/app-demo/manifest.json'), 'utf8'));

before(async () => {
  const entry = resolve(root, 'agent-demo-test.js');
  const bundle = await build({
    root, configFile: false, logLevel: 'silent',
    plugins: [svelte(), {
      name: 'agent-demo-test',
      resolveId(id) { if (id === entry) return '\0' + entry; },
      load(id) {
        if (id !== '\0' + entry) return;
        return `import Demo from ${JSON.stringify(resolve(root, 'src/components/AgentDemo.svelte'))};
          import {mount, unmount, flushSync} from 'svelte';
          export {unmount, flushSync};
          export function render(target) { return mount(Demo, {target}); }`;
      },
    }],
    build: {write: false, minify: false, lib: {entry, formats: ['es'], fileName: 'agent-demo'}},
  });
  page = new JSDOM('', {url: 'https://polymux.com', pretendToBeVisual: true});
  for (const key of ['window', 'document', 'navigator', 'Node', 'Element', 'HTMLElement', 'HTMLMediaElement', 'HTMLDialogElement', 'SVGElement', 'Text', 'Comment', 'Event', 'MouseEvent', 'CustomEvent', 'getComputedStyle', 'requestAnimationFrame', 'cancelAnimationFrame']) {
    globals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, {configurable: true, writable: true, value: key === 'window' ? page.window : page.window[key]});
  }
  const output = (Array.isArray(bundle) ? bundle[0] : bundle).output;
  const style = page.window.document.createElement('style');
  style.textContent = output.filter((file) => file.type === 'asset' && file.fileName.endsWith('.css')).map((file) => file.source).join('\n');
  page.window.document.head.append(style);
  const code = output.find((file) => file.type === 'chunk' && file.isEntry).code + '\n//# sourceURL=polymux-agent-demo-test.js';
  runtime = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
});

afterEach(async () => {
  if (instance) await runtime.unmount(instance);
  instance = undefined;
  page?.window.document.body.replaceChildren();
});

after(() => {
  page?.window.close();
  for (const [key, descriptor] of globals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else delete globalThis[key];
  }
});

function render() {
  instance = runtime.render(page.window.document.body);
  runtime.flushSync();
  return page.window.document;
}

test('the app surface is a native capture, not recreated website chrome', () => {
  const document = render();
  const preview = document.querySelector('.native-preview');
  assert.equal(preview.querySelectorAll('img[data-native-capture]').length, examples.length, 'Use a real app capture for every interactive state.');
  assert.equal(preview.querySelectorAll('svg, canvas, input, textarea, p').length, 0);
  assert.ok([...preview.querySelectorAll('button')].every((button) => button.textContent === ''));
  assert.equal(document.querySelectorAll('.demo-sidebar, .conversation-header, .messages, .reply').length, 0);
});

test('captures retain their native provenance, lossless encoding, and matching example data', () => {
  const hash = (bytes) => createHash('sha256').update(bytes).digest('hex');
  assert.equal(manifest.source, 'cua-driver.get_window_state');
  assert.equal(manifest.encoding, 'lossless-webp');
  assert.equal(manifest.windowState, 'focused', 'Capture the focused native window so its Liquid Glass controls stay active.');
  assert.equal(manifest.fixtureSha256, hash(readFileSync(resolve(root, 'src/content/demo.json'))));
  assert.equal(new Set(Object.values(manifest.captures).map((capture) => capture.pixelsSha256)).size, examples.length);
  for (const example of examples) {
    const capture = manifest.captures[example.id];
    const bytes = readFileSync(resolve(root, 'src/assets/app-demo', capture.file));
    assert.equal(hash(bytes), capture.sha256, `${example.id}: recapture changed images with prepare-demo.py`);
    assert.equal(bytes.toString('ascii', 0, 4), 'RIFF');
    assert.equal(bytes.toString('ascii', 8, 12), 'WEBP');
    const chunks = [];
    for (let offset = 12; offset + 8 <= bytes.length;) {
      chunks.push(bytes.toString('ascii', offset, offset + 4));
      const size = bytes.readUInt32LE(offset + 4);
      offset += 8 + size + (size % 2);
    }
    assert.ok(chunks.includes('VP8L'), `${example.id}: native pixels require lossless WebP`);
    assert.ok(!chunks.includes('VP8 '), `${example.id}: lossy app captures are not permitted`);
    const region = manifest.hotspots[example.id];
    assert.ok(region.x >= 0 && region.y >= 0 && region.width > 0 && region.height > 0);
    assert.ok(region.x + region.width <= manifest.width && region.y + region.height <= manifest.height);
  }
});

test('the window switches conversations from its own sidebar and keeps supplemental chrome out from under it', () => {
  const document = render();
  for (const index of [1, 2, 0]) {
    const button = document.querySelector(`.native-preview button[aria-label="Open ${examples[index].name}"]`);
    runtime.flushSync(() => button.click());
    assert.equal(document.querySelectorAll('.native-preview [aria-pressed="true"]').length, 1);
    assert.equal(document.querySelector('.native-preview img:not([hidden])').getAttribute('data-native-capture'), examples[index].id);
  }
  assert.equal(document.querySelector('.native-preview').nextElementSibling, null, 'Nothing renders under the sample window.');
  assert.equal(document.querySelectorAll('dialog, details, .zoom-button, .agent-choices, .demo-caption, .transcript, form, input, textarea').length, 0);
});

test('in-window hotspots stay invisible over the native capture', () => {
  const document = render();
  for (const button of document.querySelectorAll('.hotspots button')) {
    const style = page.window.getComputedStyle(button);
    assert.equal(style.borderTopWidth, '0px', 'Native conversation hotspots must stay invisible.');
    assert.equal(style.paddingTop, '0px');
    assert.equal(style.backgroundColor, 'rgba(0, 0, 0, 0)');
  }
});
