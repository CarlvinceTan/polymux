import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {JSDOM} from 'jsdom';
import {site, docsPath, releasePath, githubUrl} from '../lib/site.ts';
import {date, text, unique} from '../lib/content/documents.ts';
import {renderSafeMarkdown} from '../lib/content/markdown.js';
import type {BlogPost, Guide, GuideSection, PostSummary, Release, ReleaseSummary, Route, PageKind} from '../lib/content/types.ts';
import {loadContent, siteRoot} from './content.ts';

export const entries: Record<PageKind, string> = {
  home: '/src/pages/home/main.ts', blog: '/src/pages/blog/main.ts', post: '/src/pages/blog/post.ts',
  docs: '/src/pages/docs/main.ts',
  releases: '/src/pages/releases/main.ts', legal: '/src/pages/legal/main.ts',
};
export const components: Record<PageKind, string> = {
  home: '/src/pages/home/App.svelte', blog: '/src/pages/blog/BlogIndex.svelte', post: '/src/pages/blog/BlogPost.svelte',
  docs: '/src/pages/docs/DocsPage.svelte', releases: '/src/pages/releases/Releases.svelte',
  legal: '/src/pages/legal/LegalPage.svelte',
};

export function postSummary(post: BlogPost): PostSummary {
  const {body, html, toc, ...summary} = post;
  return summary;
}
export function guideSummary(page: Guide) {
  const {body, html, toc, ...summary} = page;
  return summary;
}
export function releaseSummary(release: Release): ReleaseSummary {
  const {body, html, tabs, ...summary} = release;
  return summary;
}

export function createRoutes(content: ReturnType<typeof loadContent>, root = siteRoot): Route[] {
  const metadata: Record<string, {title: string; description: string; updated?: string}> = JSON.parse(readFileSync(resolve(root, 'src/content/site.json'), 'utf8'));
  for (const [name, page] of Object.entries(metadata)) {
    text(page.title, 'title', name);
    text(page.description, 'description', name);
    if (page.updated) date(page.updated, 'updated date', name);
  }
  const staticPage = (name: string) => {
    if (!metadata[name]) throw new Error(`Missing site metadata: ${name}`);
    return metadata[name];
  };
  const route = (path: string, kind: PageKind, title: string, description: string, props: Route['props'], updated?: string): Route => ({
    path, file: path === '/' ? 'index.html' : `${path.slice(1)}index.html`, kind, title, description, props, updated,
  });
  const sections: GuideSection[] = [];
  for (const page of content.docs) {
    let section = sections.find((section) => section.title === page.section);
    if (!section) { section = {title: page.section, pages: []}; sections.push(section); }
    section.pages.push(guideSummary(page));
  }
  const routes: Route[] = [
    route('/', 'home', staticPage('home').title, metadata.home.description, {}, metadata.home.updated),
    route('/blog/', 'blog', staticPage('blog').title, metadata.blog.description, {posts: content.posts.map(postSummary)}, metadata.blog.updated),
  ];
  for (const post of content.posts) {
    const {body, ...rendered} = post;
    routes.push(route(`/blog/${post.slug}/`, 'post', `${post.title} — Polymux Blog`, post.excerpt,
      {post: rendered, related: content.posts.filter((other) => other.slug !== post.slug).slice(0, 2).map(postSummary)}, post.updated ?? post.date));
  }
  for (const [index, page] of content.docs.entries()) {
    const {body, ...rendered} = page;
    routes.push(route(docsPath(page.slug), 'docs', `${page.title} — Polymux Docs`, page.description, {
      page: rendered, sections,
      pages: content.docs.map((page) => ({...guideSummary(page), text: page.body})),
      neighbours: {
        previous: index ? guideSummary(content.docs[index - 1]) : undefined,
        next: index + 1 < content.docs.length ? guideSummary(content.docs[index + 1]) : undefined,
      },
    }, page.updated));
  }
  for (const [index, release] of content.releases.entries()) {
    const {body, ...rendered} = release;
    const props = {release: rendered, releases: content.releases.map(releaseSummary)};
    routes.push(route(releasePath(release.version), 'releases', `Polymux ${release.version} Release Notes`, release.summary, props, release.updated ?? release.date));
    if (index === 0) routes.push(route('/releases/', 'releases', staticPage('releases').title, metadata.releases.description, props, metadata.releases.updated));
  }
  if (!content.releases.length) throw new Error('The releases page requires at least one published release.');
  const dom = new JSDOM('');
  try {
    const page = staticPage('privacy-policy');
    const html = renderSafeMarkdown(readFileSync(resolve(root, 'src/content/legal/privacy-policy.html'), 'utf8'), dom.window);
    routes.push(route('/privacy-policy/', 'legal', page.title, page.description, {title: page.title.replace(' — Polymux', ''), updated: page.updated, html}, page.updated));
  } finally { dom.window.close(); }
  unique(routes.map((route) => route.path), 'route');
  return routes;
}

export function safeJson(value: unknown): string { return JSON.stringify(value).replaceAll('<', '\\u003c'); }
export function escapeHtml(value: string): string {
  return value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;');
}

export function pageHead(route: Route): string {
  const canonical = `${site.origin}${route.path}`;
  const title = escapeHtml(route.title);
  const description = escapeHtml(route.description);
  const schema = {
    '@context': 'https://schema.org', '@type': route.kind === 'post' ? 'BlogPosting' : 'WebPage',
    name: route.title, description: route.description, url: canonical,
    ...(route.updated ? {dateModified: route.updated} : {}),
    ...(route.kind === 'post' && 'post' in route.props ? {headline: route.props.post.title, datePublished: route.props.post.date, author: {'@type': 'Person', name: route.props.post.author}} : {}),
    isPartOf: {'@type': 'WebSite', '@id': `${site.origin}/#website`, name: site.name, url: `${site.origin}/`},
  };
  const structured = route.kind === 'home' ? {
    '@context': 'https://schema.org',
    '@graph': [schema,
      {'@type': 'Organization', '@id': `${site.origin}/#organization`, name: site.name, url: `${site.origin}/`, email: site.contact, sameAs: [githubUrl]},
      {'@type': 'SoftwareApplication', '@id': `${site.origin}/#software`, name: site.name, url: `${site.origin}/`,
        applicationCategory: 'BusinessApplication', operatingSystem: site.platform, description: route.description,
        downloadUrl: `${site.origin}${site.installPath}`, publisher: {'@id': `${site.origin}/#organization`}},
    ],
  } : schema;
  return `<meta name="theme-color" content="${site.themeColor}" />
    <title>${title}</title>
    <meta name="description" content="${description}" />
    <meta name="robots" content="index,follow,max-image-preview:large,max-snippet:-1" />
    <link rel="canonical" href="${canonical}" />
    <link rel="icon" type="image/png" sizes="32x32" href="/favicon-32.png?v=e5adb39a2c" />
    <link rel="sitemap" type="application/xml" href="${site.origin}/sitemap.xml" />
    <meta property="og:type" content="${route.kind === 'post' ? 'article' : 'website'}" />
    <meta property="og:site_name" content="${site.name}" />
    <meta property="og:title" content="${title}" />
    <meta property="og:description" content="${description}" />
    <meta property="og:url" content="${canonical}" />
    <meta name="twitter:card" content="summary" />
    <meta name="twitter:title" content="${title}" />
    <meta name="twitter:description" content="${description}" />
    ${route.kind === 'post' && 'post' in route.props ? `<meta property="article:published_time" content="${route.props.post.date}" />` : ''}
    <script type="application/ld+json">${safeJson(structured)}</script>
    <script id="page-data" type="application/json">${safeJson(route.props)}</script>`;
}

export function pageShell(kind: PageKind): string {
  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <!--site-head-->
  </head>
  <body data-page="${kind}">
    <div id="app"></div>
    <script type="module" src="${entries[kind]}"></script>
  </body>
</html>
`;
}

export function machineDocs(content: ReturnType<typeof loadContent>, routes: Route[]) {
  const overview = `# ${site.name}\n\n> ${routes.find((route) => route.path === '/')!.description}\n`;
  const links = content.docs.map((page) => `- [${page.title}](${site.origin}${docsPath(page.slug)}): ${page.description}`).join('\n');
  const more = `\n## More\n\n- [Native source](${githubUrl}/tree/dev)\n- [Privacy policy](${site.origin}/privacy-policy/)\n- [Release history](${site.origin}/releases/): Historical releases describe the earlier app; its distribution is retired.\n`;
  return {
    'llms.txt': `${overview}\n## Guides\n\n${links}\n${more}`,
    'llms-full.txt': `${overview}\n${content.docs.map((page) => `Source: ${site.origin}${docsPath(page.slug)}\n\n${page.body}`).join('\n\n---\n\n')}\n${more}`,
  };
}
