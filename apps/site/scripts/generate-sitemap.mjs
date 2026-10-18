import {mkdirSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {loadPublishedPosts, siteRoot} from './blog-content.mjs';
import {loadPublishedDocs} from './docs-content.mjs';
import {loadPublishedReleases} from './releases-content.mjs';
import {loadProductFeatures} from './product-content.mjs';

function urlEntry(location, lastModified) {
  return `  <url>\n    <loc>${location}</loc>\n    <lastmod>${lastModified}</lastmod>\n  </url>`;
}

export function generateSitemap() {
  const posts = loadPublishedPosts();
  const docs = loadPublishedDocs();
  const releases = loadPublishedReleases();
  const productFeatures = loadProductFeatures();
  const latestDate = [posts[0]?.date, releases[0]?.date].filter(Boolean).sort().at(-1) ?? '2026-08-28';
  const entries = [
    urlEntry('https://polymux.com/', latestDate),
    urlEntry('https://polymux.com/product/', latestDate),
    ...productFeatures.map((feature) => urlEntry(`https://polymux.com/product/${feature.slug}/`, latestDate)),
    ...docs.map((page) => urlEntry(
      page.slug === 'introduction' ? 'https://polymux.com/docs/' : `https://polymux.com/docs/${page.slug}/`,
      '2026-08-28',
    )),
    urlEntry('https://polymux.com/blog/', latestDate),
    ...posts.map((post) => urlEntry(`https://polymux.com/blog/${post.slug}/`, post.date)),
    urlEntry('https://polymux.com/releases/', releases[0]?.date ?? latestDate),
    ...releases.map((release) => urlEntry(`https://polymux.com/releases/${release.version}/`, release.date)),
    urlEntry('https://polymux.com/privacy-policy/', '2026-08-31'),
    urlEntry('https://polymux.com/privacy', '2026-10-11'),
  ];

  const sitemap = `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${entries.join('\n')}\n</urlset>\n`;
  const outputDirectory = resolve(siteRoot, 'public');
  mkdirSync(outputDirectory, {recursive: true});
  writeFileSync(resolve(outputDirectory, 'sitemap.xml'), sitemap);
}

if (process.argv[1] === import.meta.filename) generateSitemap();
