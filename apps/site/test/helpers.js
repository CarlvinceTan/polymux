import {mkdtempSync, mkdirSync, realpathSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {createRenderer} from '../scripts/renderer.ts';
import {siteRoot} from '../scripts/content.ts';
import {components} from '../scripts/routes.ts';

export function response() {
  return {statusCode: 200, headers: {}, body: '', setHeader(name, value) {this.headers[name] = value;}, end(body = '') {this.body = body;}, json() {return JSON.parse(this.body);}};
}

export async function renderer() {
  const {server, close} = await createRenderer();
  const {render} = await server.ssrLoadModule('svelte/server');
  return {close, async render(route) {
    const {default: component} = await server.ssrLoadModule(components[route.kind]);
    return render(component, {props: route.props}).body;
  }};
}

export function fixtureRoot() {
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'polymux-site-')));
  const write = (file, source) => {
    const path = resolve(root, file);
    mkdirSync(join(path, '..'), {recursive: true});
    writeFileSync(path, source);
  };
  for (const name of ['blog', 'docs', 'releases', 'legal']) mkdirSync(resolve(root, 'src/content', name), {recursive: true});
  write('src/content/docs/introduction.md', '---\ntitle: Start\nslug: introduction\ndescription: Get started\nsection: Guides\nsectionOrder: 1\norder: 1\npublished: true\n---\n# Start\n\n## Setup\n\nStart here.\n');
  write('src/content/releases/1.0.0.md', '---\nversion: 1.0.0\ntitle: Native release\ndate: 2026-10-24\nsummary: A native release\npublished: true\ndownloadable: true\n---\n## Features\n### Desktop\n- Native conversations.\n');
  write('src/content/site.json', JSON.stringify(Object.fromEntries(['home', 'blog', 'releases', 'privacy-policy'].map((name) => [name, {title: name, description: `About ${name}`}]))));
  write('src/content/legal/privacy-policy.html', '<section><h2>Your data</h2><p>Your configuration.</p></section>');
  return {root, write};
}
