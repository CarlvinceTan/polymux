import { fetch as nativeFetch } from '@tauri-apps/plugin-http';
import { isTauri } from './bridge';

export async function request(input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> {
  if (!isTauri) return fetch(input, init);
  try {
    const headers = new Headers(init.headers);
    // Native-only server routes require requests without an Origin header.
    headers.set('Origin', '');
    return await nativeFetch(input, {
      ...init,
      headers,
      maxRedirections: init.redirect === 'error' || init.redirect === 'manual' ? 0 : undefined,
    });
  } catch (error) {
    if (init.signal?.aborted) throw init.signal.reason ?? new DOMException('Aborted', 'AbortError');
    throw new TypeError('Network request failed', { cause: error });
  }
}
