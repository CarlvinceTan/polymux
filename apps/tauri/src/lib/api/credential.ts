import { invoke, isTauri } from './bridge';
import { decodeJSON } from './json';
import { request } from './transport';

/** Token and route for one server address, persisted in localStorage. */
export interface Credential {
  token: string;
  ticket?: string;
}

const key = (address: string) => `polymux.credential.${address}`;

export function loadCredential(address: string): Credential | undefined {
  const raw = localStorage.getItem(key(address));
  if (!raw) return undefined;
  try {
    const value = JSON.parse(raw) as Credential;
    return value.token ? value : undefined;
  } catch {
    return undefined;
  }
}

export function saveCredential(address: string, credential: Credential): void {
  localStorage.setItem(key(address), JSON.stringify(credential));
}

export function removeCredential(address: string): void {
  localStorage.removeItem(key(address));
}

export function addressBook(): string[] {
  const prefix = 'polymux.credential.';
  return Object.keys(localStorage)
    .filter((key) => key.startsWith(prefix))
    .map((key) => key.slice(prefix.length));
}

/** Reads the local server's owner token, so a desktop app on the same machine needs no pairing. */
export async function localToken(address: string): Promise<string | undefined> {
  if (!isTauri) return undefined;
  try {
    const token = await invoke<string | null>('local_token', { host: new URL(address).hostname });
    return token ?? undefined;
  } catch {
    return undefined;
  }
}

/** Whether the listener at `url` knows the owner token. Only an HMAC of a fresh nonce crosses the wire. */
export async function proves(token: string, url: URL): Promise<boolean> {
  const nonce = Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) =>
    b.toString(16).padStart(2, '0'),
  ).join('');
  const port = url.port || (url.protocol === 'https:' ? '443' : '80');
  try {
    const response = await request(new URL('local/proof', url), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ nonce }),
      signal: AbortSignal.timeout(3000),
      redirect: 'error',
    });
    if (!response.ok) return false;
    const proof = (await decodeJSON<{ proof?: string }>(response)).proof;
    if (!proof) return false;
    const key = await crypto.subtle.importKey(
      'raw',
      new TextEncoder().encode(token.trim()),
      { name: 'HMAC', hash: 'SHA-256' },
      false,
      ['sign'],
    );
    const mac = await crypto.subtle.sign(
      'HMAC',
      key,
      new TextEncoder().encode(`polymux-local-proof:${port}:${nonce}`),
    );
    const expected = Array.from(new Uint8Array(mac), (b) => b.toString(16).padStart(2, '0')).join('');
    return proof === expected;
  } catch {
    return false;
  }
}
