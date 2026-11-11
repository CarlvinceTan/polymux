export type TocItem = {id: string; text: string; level: 2 | 3};
export type Rendered = {html: string; toc: TocItem[]};
export type PostSummary = {
  title: string; slug: string; date: string; updated?: string; excerpt: string;
  author: string; tags: string[]; coverImage: string | null; readingMinutes: number;
};
export type BlogPost = PostSummary & Rendered & {body: string};
export type GuideSummary = {
  title: string; slug: string; description: string; section: string;
  sectionOrder: number; order: number; updated?: string;
};
export type Guide = GuideSummary & Rendered & {body: string};
export type GuideSection = {title: string; pages: GuideSummary[]};
export type GuideSearch = GuideSummary & {text: string};
export type ChangelogSection = {category: string; area: string; items: string[]};
export type ChangelogGroup = {category: string; sections: {area: string; items: {html: string}[]}[]};
export type ReleaseTab = {value: string; label: string; tabId: string; panelId: string; groups: ChangelogGroup[]};
export type ReleaseSummary = {
  version: string; title: string; date: string; updated?: string; summary: string;
  downloadable: boolean; historical: boolean; tag: string;
};
export type Release = ReleaseSummary & {body: string; html: string; tabs: ReleaseTab[]};
export type HomeProps = Record<string, never>;
export type BlogProps = {posts: PostSummary[]};
export type PostProps = {post: Omit<BlogPost, 'body'>; related: PostSummary[]};
export type DocsProps = {
  page: Omit<Guide, 'body'>; sections: GuideSection[]; pages: GuideSearch[];
  neighbours: {previous?: GuideSummary; next?: GuideSummary};
};
export type ReleaseProps = {release: Omit<Release, 'body'>; releases: ReleaseSummary[]};
export type LegalProps = {title: string; updated?: string; html: string};
export type PluginEntry = {
  name: string; title: string; description: string; author: string; kind: string; version: string; targets: string[];
  published?: string; homepage?: string; repository?: string; source?: string;
};
export type PluginProps = {plugin: PluginEntry | null; plugins: PluginEntry[]};
export type PageKind = 'home' | 'blog' | 'post' | 'docs' | 'plugins' | 'releases' | 'legal';
export type Route = {
  path: string; file: string; kind: PageKind; title: string; description: string;
  updated?: string; props: HomeProps | BlogProps | PostProps | DocsProps | PluginProps | ReleaseProps | LegalProps;
};
