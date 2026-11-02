import { ResponseError } from './errors';
import { decodeJSON } from './json';
import { saveCredential } from './credential';
import { request } from './transport';
import { serverBase } from './address';

/** Port of PolymuxKit's ServerURL.parse: a bare http(s) address, nothing else. */
export function parseAddress(value: string): URL | undefined {
  const text = value.trim();
  let url: URL;
  try {
    url = new URL(text);
  } catch {
    return undefined;
  }
  const scheme = url.protocol.replace(':', '').toLowerCase();
  if (scheme !== 'http' && scheme !== 'https') return undefined;
  if (!url.hostname || /\s/.test(url.hostname) || url.hostname.includes('%')) return undefined;
  if (url.username || url.password || url.search || url.hash) return undefined;
  if (url.port && !(+url.port >= 1 && +url.port <= 65535)) return undefined;
  if (text.slice(scheme.length + 3).split('/')[0].endsWith(':')) return undefined;
  return url;
}

export interface ParsedInvite {
  url: string;
  token: string;
  ticket?: string;
}

/** Reads a `polymux://connect` pairing link back; undefined for anything that is not one. */
export function parseInvite(link: string): ParsedInvite | undefined {
  let url: URL;
  try {
    url = new URL(link.trim());
  } catch {
    return undefined;
  }
  if (url.protocol !== 'polymux:' || url.hostname !== 'connect') return undefined;
  if (url.username || url.password || url.port || url.pathname !== '' || url.hash) return undefined;
  const items = [...url.searchParams.entries()];
  const pick = (name: string) => items.filter(([key]) => key === name);
  if (pick('url').length !== 1 || pick('token').length !== 1 || pick('ticket').length > 1) return undefined;
  const address = pick('url')[0]?.[1];
  if (!address || parseAddress(address) === undefined) return undefined;
  const token = pick('token')[0]?.[1];
  if (!token || !token.trim() || /[\s\0]/.test(token)) return undefined;
  const ticket = pick('ticket')[0]?.[1];
  if (pick('ticket').length === 1 && !ticket) return undefined;
  return { url: address, token, ticket: ticket || undefined };
}

export function inviteLink(invite: ParsedInvite): string {
  const link = new URL('polymux://connect');
  link.searchParams.set('url', invite.url);
  link.searchParams.set('token', invite.token);
  if (invite.ticket) link.searchParams.set('ticket', invite.ticket);
  return link.toString();
}

function trusted(url: URL): boolean {
  if (url.protocol === 'https:') return true;
  return url.protocol === 'http:' && (url.hostname === 'localhost' || url.hostname === '[::1]' || /^127(?:\.\d{1,3}){3}$/.test(url.hostname));
}

/**
 * Confirms an invitation before replacing the saved connection. The locally kept claim
 * makes a retry safe even when the server accepted it but its response was lost.
 */
export async function redeem(invite: ParsedInvite, signal?: AbortSignal): Promise<string> {
  const token = await claimToken(invite);
  let directError: unknown;
  if (trusted(new URL(invite.url))) {
    try {
      await exchange(invite.url, invite.token, token, signal);
      return token;
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') throw error;
      directError = error;
    }
  }
  if (!invite.ticket) {
    throw new ResponseError(
      0,
      directError instanceof Error && !(directError instanceof TypeError)
        ? directError.message
        : 'This link has no secure route. Create a new link on the server.',
    );
  }
  // Tunnelled pairing needs the iroh transport, which the Tauri shell does not embed yet.
  throw new ResponseError(400, 'This link needs a secure route the app cannot open yet. Pair while on the same network.');
}

async function exchange(base: string, code: string, token: string, signal?: AbortSignal): Promise<void> {
  const response = await request(new URL('network/pair', serverBase(base)), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Polymux-Client': 'polymux-kit' },
    body: JSON.stringify({ code, token }),
    redirect: 'error',
    signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(12000)]) : AbortSignal.timeout(12000),
  });
  if (response.status === 204) return;
  let code_: string | undefined;
  try {
    code_ = (await decodeJSON<{ code?: string }>(response)).code;
  } catch {
    // fall through to the generic message
  }
  if (code_ === 'pair_used' || code_ === 'pair_expired') {
    throw new ResponseError(response.status, 'This pairing link expired or was already used. Create a new link in Connections.');
  }
  throw new ResponseError(response.status, "Couldn't pair with this server. Create a new link in Connections.");
}

async function claimKey(invite: ParsedInvite): Promise<string> {
  const bytes = new TextEncoder().encode(JSON.stringify([invite.url, invite.token, invite.ticket ?? '']));
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return `polymux.pairing.claim.${Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('')}`;
}

/** A locally kept claim token, so a retried pair proves it is the same client. */
async function claimToken(invite: ParsedInvite): Promise<string> {
  const key = await claimKey(invite);
  const existing = localStorage.getItem(key);
  if (existing && existing.length === 43) return existing;
  const bytes = crypto.getRandomValues(new Uint8Array(32));
  const token = btoa(String.fromCharCode(...bytes))
    .replaceAll('+', '-')
    .replaceAll('/', '_')
    .replaceAll('=', '');
  localStorage.setItem(key, token);
  return token;
}

/** Commits a successful pairing as the saved connection for that address. */
export async function finish(invite: ParsedInvite, token: string): Promise<void> {
  saveCredential(serverBase(invite.url).toString(), { token, ticket: invite.ticket });
  localStorage.removeItem(await claimKey(invite));
}
