import {site} from '../site.ts';

export function githubClient({request = globalThis.fetch, timeoutMs = 10_000, cacheMs = 60_000} = {}) {
  const cache = new Map<string, {expires: number; value: unknown}>();
  const pending = new Map<string, Promise<unknown>>();
  async function fetchJson(path: string): Promise<unknown> {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const deadline = new Promise<null>((resolve) => {
      timer = setTimeout(() => { controller.abort(); resolve(null); }, timeoutMs);
    });
    try {
      const operation = request(`https://api.github.com/repos/${site.repository}/${path}`, {
        signal: controller.signal,
        headers: {Accept: 'application/vnd.github+json', 'User-Agent': 'Polymux-Website', 'X-GitHub-Api-Version': '2022-11-28'},
      }).then((response) => response.ok ? response.json() : null).catch(() => null);
      const value: unknown = await Promise.race([operation, deadline]);
      if (value !== null) cache.set(path, {value, expires: Date.now() + cacheMs});
      return value;
    } catch { return null; }
    finally { clearTimeout(timer!); pending.delete(path); }
  }
  return async (path: string): Promise<unknown> => {
    const saved = cache.get(path);
    if (saved && saved.expires > Date.now()) return saved.value;
    cache.delete(path);
    if (!pending.has(path)) pending.set(path, fetchJson(path));
    return pending.get(path)!;
  };
}
