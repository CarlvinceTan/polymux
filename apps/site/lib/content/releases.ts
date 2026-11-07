import {date, text, updated} from './documents.ts';
import type {ReleaseSummary} from './types.ts';

export function readRelease(metadata: Record<string, unknown>, filename: string): ReleaseSummary {
  const version = text(metadata.version, 'version', filename);
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) throw new Error(`${filename} has an invalid version.`);
  for (const field of ['downloadable', 'historical']) {
    if (metadata[field] !== undefined && typeof metadata[field] !== 'boolean') throw new Error(`${filename}: ${field} must be a boolean.`);
  }
  const tagValue = metadata.tag;
  const tag = text(tagValue === undefined || tagValue === null || tagValue === '' ? `v${version}` : tagValue, 'tag', filename);
  if (!/^[\w.+-]{1,80}$/.test(tag)) throw new Error(`${filename} has an invalid release tag.`);
  if (metadata.historical === true && metadata.downloadable === true) throw new Error(`${filename}: historical distribution is retired, so downloadable must be false.`);
  return {
    version, tag, title: text(metadata.title, 'title', filename), date: date(metadata.date, 'date', filename),
    updated: updated(metadata.updated, filename), summary: text(metadata.summary, 'summary', filename),
    downloadable: metadata.downloadable === true, historical: metadata.historical === true,
  };
}
