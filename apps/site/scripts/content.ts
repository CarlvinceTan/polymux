import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {JSDOM} from 'jsdom';
import {readFeatures, text, slug, date, updated, unique, integer} from '../lib/content/documents.ts';
import {readRelease} from '../lib/content/releases.ts';
import {site} from '../lib/site.ts';
import {ALL_PLATFORMS, parseReleaseChangelog, groupReleaseChangelog, filterReleaseChangelog} from '../lib/content/changelog.ts';
import {renderSafeMarkdown} from '../lib/content/markdown.js';
import type {BlogPost, Guide, ProductFeature, Release, Rendered, ReleaseTab} from '../lib/content/types.ts';

import {readCollection, siteRoot} from './files.ts';
export {siteRoot} from './files.ts';
export const docsContentRoot = resolve(siteRoot, 'src/content/docs');
export const releasesContentRoot = resolve(siteRoot, 'src/content/releases');

function renderDocument(body: string, window: JSDOM['window']): Rendered {
  const element = window.document.createElement('article');
  element.innerHTML = renderSafeMarkdown(body, window);
  const used = new Set<string>();
  const toc = [...element.querySelectorAll('h2, h3')].map((heading) => {
    const label = heading.textContent?.trim() ?? '';
    const base = label.toLowerCase().replace(/[^a-z0-9\s-]/g, '').trim().replace(/\s+/g, '-').replace(/-+/g, '-') || 'section';
    let id = base;
    for (let suffix = 2; used.has(id); suffix++) id = `${base}-${suffix}`;
    used.add(id);
    heading.id = id;
    return {id, text: label, level: Number(heading.tagName.slice(1)) as 2 | 3};
  });
  return {html: element.innerHTML, toc};
}

export function loadContent(root = siteRoot) {
  const dom = new JSDOM('');
  try {
    const render = (body: string) => renderDocument(body, dom.window);
    const posts: BlogPost[] = readCollection(root, 'blog').map(({metadata, body, filename}) => ({
      title: text(metadata.title, 'title', filename), slug: slug(metadata.slug ?? filename.replace(/\.md$/, ''), filename),
      date: date(metadata.date, 'date', filename), updated: updated(metadata.updated, filename),
      excerpt: text(metadata.excerpt, 'excerpt', filename), author: text(metadata.author ?? 'Polymux', 'author', filename),
      tags: Array.isArray(metadata.tags) ? metadata.tags.map((tag) => text(tag, 'tag', filename)) : [],
      coverImage: metadata.coverImage ? text(metadata.coverImage, 'coverImage', filename) : null,
      readingMinutes: Math.max(1, Math.ceil(body.split(/\s+/).filter(Boolean).length / 220)), body, ...render(body),
    })).sort((a, b) => b.date.localeCompare(a.date));
    const docs: Guide[] = readCollection(root, 'docs').map(({metadata, body, filename}) => ({
      title: text(metadata.title, 'title', filename), slug: slug(metadata.slug ?? filename.replace(/\.md$/, ''), filename),
      description: text(metadata.description, 'description', filename), section: text(metadata.section, 'section', filename),
      sectionOrder: integer(metadata.sectionOrder, 'sectionOrder', filename), order: integer(metadata.order, 'order', filename),
      updated: updated(metadata.updated, filename), body, ...render(body),
    })).sort((a, b) => a.sectionOrder - b.sectionOrder || a.order - b.order || a.title.localeCompare(b.title));
    const releases: Release[] = readCollection(root, 'releases').map(({metadata, body, filename}) => {
      const summary = readRelease(metadata, filename);
      const sections = parseReleaseChangelog(body, filename);
      const platforms = [...new Set(sections.map((section) => section.area))];
      const tabs: ReleaseTab[] = [ALL_PLATFORMS, ...platforms].map((value, index) => ({
        value, label: value === ALL_PLATFORMS ? 'All' : value,
        tabId: `release-tab-${index}`, panelId: `release-panel-${index}`,
        groups: groupReleaseChangelog(filterReleaseChangelog(sections, value)).map((group) => ({
          category: group.category,
          sections: group.sections.map((section) => ({area: section.area, items: section.items.map((item) => ({html: renderSafeMarkdown(item, dom.window)}))})),
        })),
      }));
      return {
        ...summary,
        body, html: render(body).html, tabs,
      };
    }).sort((a, b) => b.date.localeCompare(a.date) || b.version.localeCompare(a.version, undefined, {numeric: true}));
    const features = readFeatures(JSON.parse(readFileSync(resolve(root, 'src/content/product/features.json'), 'utf8')));
    unique(posts.map((post) => post.slug), 'blog slug');
    unique(docs.map((page) => page.slug), 'documentation slug');
    unique(releases.map((release) => release.version), 'release version');
    if (!docs.some((page) => page.slug === 'introduction')) throw new Error('Documentation requires an introduction.');
    for (const feature of features) checkGuide(feature.docsPath, docs, feature.slug);
    return {posts, docs, releases, features};
  } finally { dom.window.close(); }
}

export function checkGuide(href: string, docs: Guide[], source: string) {
  const url = new URL(href, site.origin);
  const guide = docs.find((page) => page.slug === (url.pathname.split('/')[2] || 'introduction'));
  if (!guide || (url.hash && !guide.toc.some((heading) => `#${heading.id}` === url.hash))) {
    throw new Error(`${source}: missing guide target ${href}`);
  }
}

export function loadPublishedPosts(root = siteRoot) { return loadContent(root).posts; }
export function loadPublishedDocs(root = siteRoot) { return loadContent(root).docs; }
export function loadPublishedReleases(root = siteRoot) { return loadContent(root).releases; }
export function loadProductFeatures(root = siteRoot): ProductFeature[] { return loadContent(root).features; }
export {validateReleaseBody} from '../lib/content/changelog.ts';
