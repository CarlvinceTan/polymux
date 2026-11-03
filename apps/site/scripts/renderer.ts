import {mkdtempSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createServer} from 'vite';
import {svelte} from '@sveltejs/vite-plugin-svelte';
import {siteRoot} from './files.ts';

export async function createRenderer(root = siteRoot) {
  const cacheDir = mkdtempSync(join(tmpdir(), 'polymux-render-'));
  try {
    const server = await createServer({root, cacheDir, configFile: false, logLevel: 'error', plugins: [svelte()],
      server: {middlewareMode: true, watch: null, hmr: false, ws: false}, appType: 'custom'});
    return {server, async close() {
      try {await server.close();}
      finally {rmSync(cacheDir, {recursive: true, force: true});}
    }};
  } catch (error) {
    rmSync(cacheDir, {recursive: true, force: true});
    throw error;
  }
}
