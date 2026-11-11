export const site = {
  name: 'Polymux',
  origin: 'https://polymux.com',
  repository: 'CarlvinceTan/polymux',
  installPath: '/docs/installation/',
  themeColor: '#0d0d0f',
  platform: 'macOS',
  contact: 'carlvince@polymux.com',
};

export const githubUrl = `https://github.com/${site.repository}`;
export const docsPath = (slug: string) => slug === 'introduction' ? '/docs/' : `/docs/${slug}/`;
export const releasePath = (version: string) => `/releases/${encodeURIComponent(version)}/`;
export const pluginPath = (name: string) => `/plugins/${encodeURIComponent(name)}/`;
export const navigation = [
  {id: 'home', label: 'Home', href: '/'},
  {id: 'plugins', label: 'Plugins', href: '/plugins/'},
  {id: 'docs', label: 'Docs', href: '/docs/'},
  {id: 'blog', label: 'Blog', href: '/blog/'},
  {id: 'releases', label: 'Releases', href: '/releases/'},
] as const;

export type Section = typeof navigation[number]['id'];
