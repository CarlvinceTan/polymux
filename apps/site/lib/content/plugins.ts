import {existsSync, readFileSync} from 'node:fs';
import type {PluginEntry} from './types.ts';

const validName = /^[a-z0-9][a-z0-9-]{0,39}$/;

type Release = {
  version: string; publishTimestamp?: number;
  source?: {repository?: string; commit?: string; artifacts?: Record<string, unknown>};
};
type Entry = {
  name: string; title?: string; description?: string; author?: string; homepage?: string; kind: string; versions?: Release[];
};

const https = (value = '') => value.startsWith('https://') ? value : undefined;

/** The newest release with packages: stable when there is one, as the server recommends. */
function latest(versions: Release[]): Release | undefined {
  const ready = versions.filter((release) => Object.keys(release.source?.artifacts ?? {}).length);
  const stable = ready.filter((release) => !release.version.includes('-'));
  return (stable.length ? stable : ready)
    .sort((a, b) => a.version.localeCompare(b.version, undefined, {numeric: true})).at(-1);
}

function present(entry: Entry): PluginEntry | undefined {
  if (!validName.test(entry.name)) throw new Error(`Plugin name "${entry.name}" is invalid.`);
  if (entry.kind !== 'global' && entry.kind !== 'scoped') throw new Error(`Plugin ${entry.name} has an invalid kind.`);
  const release = latest(entry.versions ?? []);
  if (!release) return undefined;
  const {repository = '', commit = '', artifacts = {}} = release.source ?? {};
  const repo = /^[\w.-]+\/[\w.-]+$/.test(repository) ? `https://github.com/${repository}` : undefined;
  return {
    name: entry.name, title: entry.title || entry.name, description: entry.description ?? '', author: entry.author ?? '',
    kind: entry.kind, version: release.version, targets: Object.keys(artifacts).sort(),
    published: release.publishTimestamp ? new Date(release.publishTimestamp * 1000).toISOString().slice(0, 10) : undefined,
    homepage: https(entry.homepage), repository: repo, source: repo && commit ? `${repo}/tree/${commit}` : repo,
  };
}

export function loadPlugins(file: string): PluginEntry[] {
  // Only a build fetches the registry; tests and the dev server run without one.
  if (!existsSync(file)) return [];
  const index = JSON.parse(readFileSync(file, 'utf8')) as {format: number; plugins: Entry[]};
  if (index.format !== 1) throw new Error(`Plugin registry format ${index.format} isn't supported.`);
  return index.plugins.map(present).filter((plugin) => plugin !== undefined);
}
