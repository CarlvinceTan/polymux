import {lstatSync, readFileSync, readdirSync} from 'node:fs';
import {dirname, resolve, sep} from 'node:path';
import {readDocument, unique} from '../lib/content/documents.ts';
import {readRelease} from '../lib/content/releases.ts';
import {validateReleaseBody} from '../lib/content/changelog.ts';

export const siteRoot = resolve(import.meta.dirname, '..');

export function readCollection(root: string, collection: string) {
  const directory = resolve(root, 'src/content', collection);
  return readdirSync(directory).filter((name) => name.endsWith('.md')).sort().flatMap((filename) => {
    const document = readDocument(readFileSync(resolve(directory, filename), 'utf8'), filename);
    return document.published ? [{...document, filename}] : [];
  });
}

export function readReleaseData(root = siteRoot) {
  const releases = readCollection(root, 'releases').map(({metadata, body, filename}) => {
    validateReleaseBody(filename, body);
    return readRelease(metadata, filename);
  }).sort((a, b) => b.date.localeCompare(a.date) || b.version.localeCompare(a.version, undefined, {numeric: true}));
  unique(releases.map((release) => release.version), 'release version');
  return releases;
}

export function outputPath(root: string, file: string): string {
  const path = resolve(root, file);
  if (!path.startsWith(`${resolve(root)}${sep}`)) throw new Error(`Generated path escapes the site: ${file}`);
  let current = path;
  while (current !== resolve(root)) {
    if (lstatSync(current, {throwIfNoEntry: false})?.isSymbolicLink()) throw new Error(`Generated path contains a symlink: ${current}`);
    current = dirname(current);
  }
  return path;
}
