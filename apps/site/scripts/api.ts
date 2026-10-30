import {mkdirSync, renameSync, rmSync, writeFileSync} from 'node:fs';
import {dirname} from 'node:path';
import {randomUUID} from 'node:crypto';
import type {ReleaseSummary} from '../lib/content/types.ts';
import {outputPath, readReleaseData, siteRoot} from './files.ts';

export function writeReleaseData(root = siteRoot, releases: ReleaseSummary[] = readReleaseData(root)) {
  const path = outputPath(root, '.generated/releases.json');
  mkdirSync(dirname(path), {recursive: true});
  const temporary = `${path}.${randomUUID()}.tmp`;
  try {
    writeFileSync(temporary, `${JSON.stringify(releases, null, 2)}\n`, {flag: 'wx'});
    renameSync(temporary, path);
  } finally {rmSync(temporary, {force: true});}
}

if (process.argv[1] === import.meta.filename) writeReleaseData(process.argv[2] ?? siteRoot);
