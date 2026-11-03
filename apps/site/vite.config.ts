import {defineConfig} from 'vite';
import {svelte} from '@sveltejs/vite-plugin-svelte';
import {resolve} from 'node:path';
import {pagesRoot, prepare} from './scripts/prepare.ts';
import {siteRoot} from './scripts/content.ts';
import {assetLinks, previewPlugin, sitePlugin} from './scripts/vite.ts';

export default defineConfig(({isPreview}) => {
  if (isPreview) return {appType: 'mpa', build: {outDir: resolve(siteRoot, 'dist')}, plugins: [previewPlugin()]};
  const prepared = prepare();
  return {
    appType: 'mpa',
    plugins: [sitePlugin(prepared), svelte(), assetLinks()],
    // Polls, because macOS file events can arrive many seconds late on a busy machine.
    server: {watch: {usePolling: true}},
    build: {
      outDir: resolve(siteRoot, 'dist'),
      emptyOutDir: true,
      rolldownOptions: {
        input: prepared.routes.map((route) => resolve(pagesRoot(), route.file)),
      },
    },
  };
});
