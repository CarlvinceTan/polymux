import {readFileSync} from 'node:fs';
import {relative, resolve, sep} from 'node:path';
import {type Plugin, type ViteDevServer, type PreviewServer} from 'vite';
import {createDownloads} from '../lib/server/downloads.ts';
import {retire} from '../lib/server/http.ts';
import {components, machineDocs, pageHead, releaseSummary} from './routes.ts';
import {pagesRoot, prepare} from './prepare.ts';
import {siteRoot} from './content.ts';
import {createRenderer} from './renderer.ts';
import type {ReleaseSummary} from '../lib/content/types.ts';

type Hosting = {
  redirects?: {source: string; destination: string; permanent: boolean}[];
  headers?: {source: string; headers: {key: string; value: string}[]}[];
};
const config: Hosting = JSON.parse(readFileSync(resolve(siteRoot, 'vercel.json'), 'utf8'));
export const hosting: Hosting = {redirects: config.redirects, headers: config.headers};
export const normalizePath = (path: string) => path.replace(/index\.html$/, '').replace(/\/$/, '') || '/';

function registerMiddleware(server: ViteDevServer | PreviewServer, options: {
  hosting: Hosting;
  download: ReturnType<typeof createDownloads>;
  docs?: () => ReturnType<typeof machineDocs>;
  sitemap?: () => string;
}) {
  server.middlewares.use(async (request, response, next) => {
    const url = new URL(request.url ?? '/', 'http://localhost');
    const path = normalizePath(url.pathname);
    for (const rule of options.hosting.headers ?? []) {
      if (rule.source === '/(.*)') {
        for (const header of rule.headers) response.setHeader(header.key, header.value);
      }
    }
    const redirect = options.hosting.redirects?.find((redirect) => normalizePath(redirect.source) === path);
    if ((path === '/llms.txt' || path === '/llms-full.txt') && options.docs) {
      response.setHeader('Content-Type', 'text/plain; charset=utf-8');
      response.end(options.docs()[path.slice(1) as 'llms.txt' | 'llms-full.txt']);
    } else if (path === '/sitemap.xml' && options.sitemap) {
      response.setHeader('Content-Type', 'application/xml; charset=utf-8');
      response.end(options.sitemap());
    } else if (redirect) {
      response.statusCode = redirect.permanent ? 308 : 307;
      response.setHeader('Location', redirect.destination);
      response.end();
    } else if (path === '/install.sh' || path === '/api/install') retire(response, true);
    else if (['/api/releases', '/api/updates/win32/x64/RELEASES', '/api/updates/linux/x64/latest-linux.yml'].includes(path)) retire(response);
    else if (path === '/api/downloads') {
      try {await options.download({query: Object.fromEntries(url.searchParams)}, response);}
      catch (error) {next(error as Error);}
    } else next();
  });
}

export function sitePlugin(initial = prepare(), root = siteRoot): Plugin {
  let prepared = initial;
  let renderer: ViteDevServer | undefined;
  let rendering: ReturnType<typeof createRenderer> | undefined;
  const renderServer = async () => {
    if (renderer) return renderer;
    rendering ??= createRenderer(root);
    return (await rendering).server;
  };
  let downloads = createDownloads(prepared.content.releases.map(releaseSummary));
  return {
    name: 'polymux-site',
    config() {
      return {
        root: pagesRoot(root),
        publicDir: resolve(root, 'public'),
        cacheDir: resolve(root, '.generated/vite'),
        resolve: {alias: {'/src': resolve(root, 'src')}},
        server: {fs: {allow: [root]}},
      };
    },
    generateBundle() {
      this.emitFile({type: 'asset', fileName: 'sitemap.xml', source: prepared.sitemap});
      for (const [fileName, source] of Object.entries(machineDocs(prepared.content, prepared.routes))) {
        this.emitFile({type: 'asset', fileName, source});
      }
      this.emitFile({type: 'asset', fileName: '_site/manifest.json', source: JSON.stringify({hosting, releases: prepared.content.releases.map(releaseSummary)})});
    },
    transformIndexHtml: {
      order: 'pre',
      async handler(html, context) {
        const route = prepared.routes.find((route) => normalizePath(route.path) === normalizePath(context.path));
        if (!route) return html;
        const server = await renderServer();
        const [{default: component}, {render}] = await Promise.all([
          server.ssrLoadModule(components[route.kind]), server.ssrLoadModule('svelte/server'),
        ]);
        const rendered = render(component, {props: route.props});
        return html.replace('<!--site-head-->', `${pageHead(route)}${rendered.head}`)
          .replace('<div id="app"></div>', `<div id="app">${rendered.body}</div>`);
      },
    },
    configureServer(server) {
      renderer = server;
      registerMiddleware(server, {hosting, download: (...args) => downloads(...args), docs: () => machineDocs(prepared.content, prepared.routes), sitemap: () => prepared.sitemap});
      const directory = resolve(root, 'src/content');
      server.watcher.add(directory);
      let timer: ReturnType<typeof setTimeout> | undefined;
      const changed = (file: string) => {
        if (!file.startsWith(`${directory}/`)) return;
        clearTimeout(timer);
        timer = setTimeout(() => {
          try {
            prepared = prepare({root});
            downloads = createDownloads(prepared.content.releases.map(releaseSummary));
            server.ws.send({type: 'full-reload'});
          } catch (error) {
            server.ws.send({type: 'error', err: {message: (error as Error).message, stack: (error as Error).stack ?? ''}});
          }
        }, 50);
      };
      for (const event of ['add', 'change', 'unlink'] as const) server.watcher.on(event, changed);
      server.httpServer?.once('close', () => {
        clearTimeout(timer);
        for (const event of ['add', 'change', 'unlink'] as const) server.watcher.off(event, changed);
      });
    },
    async closeBundle() { if (rendering) await (await rendering).close(); },
  };
}

export function previewPlugin(): Plugin {
  return {
    name: 'polymux-preview',
    configurePreviewServer(server) {
      const path = resolve(server.config.root, server.config.build.outDir, '_site/manifest.json');
      const snapshot: {hosting: Hosting; releases: ReleaseSummary[]} = JSON.parse(readFileSync(path, 'utf8'));
      registerMiddleware(server, {hosting: snapshot.hosting, download: createDownloads(snapshot.releases)});
    },
  };
}

export function assetLinks(root = siteRoot): Plugin {
  let buildRoot = root;
  return {
    name: 'polymux-asset-links',
    configResolved(config) {buildRoot = config.root;},
    transformIndexHtml: {
      order: 'post',
      handler(html, context) {
        if (!context.bundle) return html;
        const assets = Object.values(context.bundle).filter((file) => file.type === 'asset').flatMap((file) =>
          file.originalFileNames.map((source) => [`/${relative(root, resolve(buildRoot, source)).split(sep).join('/')}`, `/${file.fileName}`] as const));
        return html.replace(/(?:href|src|style)="[^"]*"/g, (attribute) => {
          for (const [source, destination] of assets) attribute = attribute.replaceAll(source, destination);
          return attribute;
        });
      },
    },
  };
}
