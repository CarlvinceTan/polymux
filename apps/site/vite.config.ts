import {defineConfig} from 'vite';
import {svelte} from '@sveltejs/vite-plugin-svelte';
import {existsSync, readdirSync} from 'node:fs';
import {resolve} from 'node:path';

const blogRoot = resolve(import.meta.dirname, 'blog');
const blogPosts = existsSync(blogRoot)
  ? readdirSync(blogRoot, {withFileTypes: true})
      .filter((entry) => entry.isDirectory() && existsSync(resolve(blogRoot, entry.name, 'index.html')))
      .map((entry) => [
        `blog-${entry.name}`,
        resolve(blogRoot, entry.name, 'index.html'),
      ] as const)
  : [];

const docsRoot = resolve(import.meta.dirname, 'docs');
const docsPages = existsSync(docsRoot)
  ? readdirSync(docsRoot, {withFileTypes: true})
      .filter((entry) => entry.isDirectory() && existsSync(resolve(docsRoot, entry.name, 'index.html')))
      .map((entry) => [
        `docs-${entry.name}`,
        resolve(docsRoot, entry.name, 'index.html'),
      ] as const)
  : [];

const releasesRoot = resolve(import.meta.dirname, 'releases');
const releasePages = existsSync(releasesRoot)
  ? readdirSync(releasesRoot, {withFileTypes: true})
      .filter((entry) => entry.isDirectory() && existsSync(resolve(releasesRoot, entry.name, 'index.html')))
      .map((entry) => [
        `release-${entry.name}`,
        resolve(releasesRoot, entry.name, 'index.html'),
      ] as const)
  : [];

const productRoot = resolve(import.meta.dirname, 'product');
const productPages = existsSync(productRoot)
  ? readdirSync(productRoot, {withFileTypes: true})
      .filter((entry) => entry.isDirectory() && existsSync(resolve(productRoot, entry.name, 'index.html')))
      .map((entry) => [
        `product-${entry.name}`,
        resolve(productRoot, entry.name, 'index.html'),
      ] as const)
  : [];

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: 'dist',
    rollupOptions: {
      input: {
        main: resolve(import.meta.dirname, 'index.html'),
        admin: resolve(import.meta.dirname, 'admin/index.html'),
        blog: resolve(import.meta.dirname, 'blog/index.html'),
        docs: resolve(import.meta.dirname, 'docs/index.html'),
        releases: resolve(import.meta.dirname, 'releases/index.html'),
        product: resolve(import.meta.dirname, 'product/index.html'),
        privacy: resolve(import.meta.dirname, 'privacy/index.html'),
        privacyPolicy: resolve(import.meta.dirname, 'privacy-policy/index.html'),
        ...Object.fromEntries(blogPosts),
        ...Object.fromEntries(docsPages),
        ...Object.fromEntries(releasePages),
        ...Object.fromEntries(productPages),
      },
    },
  },
});
