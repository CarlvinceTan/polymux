import {marked, type Tokens} from 'marked';
import type {ChangelogSection} from './types.ts';

export const ALL_PLATFORMS = 'all';
const categories = new Set(['Features', 'Bug Fixes', 'Improvements']);

export function parseReleaseChangelog(body: string, filename = 'Release'): ChangelogSection[] {
  const sections: ChangelogSection[] = [];
  let category = '';
  let area = '';
  for (const token of marked.lexer(body)) {
    if (token.type === 'space') continue;
    if (token.type === 'heading' && token.depth === 2 && categories.has(token.text)) {
      category = token.text;
      area = '';
    } else if (token.type === 'heading' && token.depth === 3 && category) {
      area = token.text.trim();
    } else if (token.type === 'list' && !token.ordered && category && area) {
      let section = sections.at(-1);
      if (section?.category !== category || section.area !== area) {
        section = {category, area, items: []};
        sections.push(section);
      }
      section.items.push(...token.items.map((item: Tokens.ListItem) => item.text));
    } else {
      throw new Error(`${filename}: use category and area headings followed by bullet lists; unsupported ${token.type}.`);
    }
  }
  if (!sections.some((section) => section.items.length)) throw new Error(`${filename} must contain at least one release-note bullet.`);
  return sections;
}

export function validateReleaseBody(filename: string, body: string) {
  parseReleaseChangelog(body, filename);
}

export function releaseChangelog(entry: {body: string}) { return parseReleaseChangelog(entry.body); }
export function releasePlatforms(entry: {body: string}) { return [...new Set(releaseChangelog(entry).map((section) => section.area))]; }
export function filterReleaseChangelog(sections: ChangelogSection[], platform = ALL_PLATFORMS) {
  return !platform || platform === ALL_PLATFORMS ? sections : sections.filter((section) => section.area === platform);
}
export function resolveReleasePlatform(platforms: string[], requested?: string) {
  return requested && platforms.includes(requested) ? requested : ALL_PLATFORMS;
}
export function groupReleaseChangelog(sections: ChangelogSection[]) {
  const groups: {category: string; sections: ChangelogSection[]}[] = [];
  for (const section of sections) {
    let group = groups.find((group) => group.category === section.category);
    if (!group) { group = {category: section.category, sections: []}; groups.push(group); }
    const existing = group.sections.find((entry) => entry.area === section.area);
    if (existing) existing.items.push(...section.items);
    else group.sections.push({...section, items: [...section.items]});
  }
  return groups;
}
