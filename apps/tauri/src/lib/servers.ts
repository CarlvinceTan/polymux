export interface SavedServer {
  address: string;
  name: string;
}

const key = 'polymux.servers';

export function savedServers(): SavedServer[] {
  try {
    const value = JSON.parse(localStorage.getItem(key) ?? '[]') as unknown;
    if (!Array.isArray(value)) return [];
    return value.flatMap((item) => {
      if (!item || typeof item !== 'object') return [];
      const address = typeof (item as SavedServer).address === 'string' ? (item as SavedServer).address : '';
      const name = typeof (item as SavedServer).name === 'string' ? (item as SavedServer).name : '';
      return address ? [{ address, name: name || address.replace(/^https?:\/\//, '') }] : [];
    });
  } catch { return []; }
}

export function rememberServer(address: string, name?: string): SavedServer[] {
  const clean = address.replace(/\/$/, '');
  const existing = savedServers().filter((server) => server.address !== clean);
  const next = [{ address: clean, name: name?.trim() || clean.replace(/^https?:\/\//, '') }, ...existing].slice(0, 12);
  try { localStorage.setItem(key, JSON.stringify(next)); } catch { /* The connection still works without a saved list. */ }
  return next;
}

export function forgetServer(address: string): SavedServer[] {
  const next = savedServers().filter((server) => server.address !== address);
  try { localStorage.setItem(key, JSON.stringify(next)); } catch { /* The in-memory list remains. */ }
  return next;
}
