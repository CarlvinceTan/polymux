import {mkdirSync, writeFileSync} from 'node:fs';
import {dirname} from 'node:path';
import {pluginRegistry} from './content.ts';

// The registry's source. polymux.com/plugins/index.json (vercel.json) serves the same file, which is what servers read by default.
export const registryAddress = 'https://raw.githubusercontent.com/CarlvinceTan/polymux/dev/plugins/index.json';

/** Saves the official registry for a build. A failure stops the build rather than publishing an empty list. */
export async function fetchRegistry(path = pluginRegistry(), load = fetch) {
  const response = await load(registryAddress, {signal: AbortSignal.timeout(20_000)});
  if (!response.ok) throw new Error(`The plugin registry answered ${response.status}.`);
  mkdirSync(dirname(path), {recursive: true});
  writeFileSync(path, await response.text());
}

if (process.argv[1] === import.meta.filename && !process.env.POLYMUX_SITE_REGISTRY) await fetchRegistry();
