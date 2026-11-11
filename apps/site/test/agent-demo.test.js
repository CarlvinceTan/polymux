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
  page.window.HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', ''); };
  page.window.HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
    this.dispatchEvent(new page.window.Event('close'));
  };
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

test('agent selection changes the native capture and guide without replacing the focused control', () => {
  const document = render();
  const buttons = [...document.querySelectorAll('.agent-choices button')];
  for (const index of [1, 2, 0, 2]) {
    const button = buttons[index];
    button.focus();
    runtime.flushSync(() => button.click());
    assert.equal(document.activeElement, button);
    assert.equal(document.querySelectorAll('.agent-choices [aria-pressed="true"]').length, 1);
    assert.equal(button.getAttribute('aria-pressed'), 'true');
    const image = document.querySelector('.native-preview img:not([hidden])');
    assert.equal(image.getAttribute('data-native-capture'), examples[index].id);
    assert.ok(image.alt.includes(examples[index].name));
    assert.ok(document.querySelector('.transcript').textContent.includes(examples[index].prompt));
    assert.equal(document.querySelector('.demo-guide').getAttribute('href'), examples[index].guide);
  }
  runtime.flushSync(() => document.querySelector('.native-preview button[aria-label="Open Nova"]').click());
  assert.equal(document.querySelector('.native-preview img:not([hidden])').getAttribute('data-native-capture'), 'nova');
});

test('zoom shares the selected native state and returns keyboard focus when closed', () => {
  const document = render();
  const zoom = document.querySelector('.zoom-button');
  zoom.focus();
  runtime.flushSync(() => zoom.click());
  const dialog = document.querySelector('dialog');
  assert.equal(dialog.open, true);
  runtime.flushSync(() => dialog.querySelector('button[aria-label="Open Fern"]').click());
  for (const surface of ['.native-preview', 'dialog']) {
    assert.equal(document.querySelector(`${surface} img:not([hidden])`).getAttribute('data-native-capture'), 'fern');
  }
  runtime.flushSync(() => dialog.querySelector('header button').click());
  assert.equal(dialog.open, false);
  assert.equal(document.activeElement, zoom);
});

test('zoom controls do not paint over or stretch native captures', () => {
  const document = render();
  for (const button of document.querySelectorAll('.hotspots button')) {
    const style = page.window.getComputedStyle(button);
    assert.equal(style.borderTopWidth, '0px', 'Native conversation hotspots must stay invisible in both surfaces.');
    assert.equal(style.paddingTop, '0px');
    assert.equal(style.backgroundColor, 'rgba(0, 0, 0, 0)');
  }
  const capture = document.querySelector('.capture.expanded');
  const expanded = page.window.getComputedStyle(capture);
  // jsdom 31 resolves the custom property, so compare against its value rather than the raw var().
  assert.equal(expanded.width, expanded.getPropertyValue('--native-width').trim());
  assert.notEqual(expanded.minWidth, '100%', 'Zoom must not upscale a native capture to fill the dialog.');
});

test('captures stay explicitly local and provide a readable transcript with the real boundaries', () => {
  const document = render();
  const buttons = [...document.querySelectorAll('.agent-choices button')];
  assert.match(document.querySelector('.demo-note').textContent, /Real app renders with example data\. Nothing is sent\./);
  assert.equal(document.querySelectorAll('form, input, textarea').length, 0);
  runtime.flushSync(() => buttons[1].click());
  assert.match(document.querySelector('.transcript').textContent, /Saved for this conversation/);
  runtime.flushSync(() => buttons[2].click());
  assert.match(document.querySelector('.transcript').textContent, /server needs to stay running/);
  assert.equal(document.querySelector('.transcript > div').getAttribute('aria-live'), 'polite');
});
