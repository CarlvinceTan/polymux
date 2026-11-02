import assert from 'node:assert/strict';
import test from 'node:test';
import { mockIPC } from '@tauri-apps/api/mocks';

Object.assign(globalThis, { window: {} });
mockIPC(() => undefined);
const { request } = await import('./transport');
const { APIClient } = await import('./client');
const { decodeSSE } = await import('./sse');

test('remote servers do not receive the local owner token or a local proof request', async () => {
  const calls: string[] = [];
  let read = false;
  Object.assign(globalThis, { localStorage: {
    getItem: () => JSON.stringify({ token: 'paired-token' }),
  } });
  mockIPC((command, payload) => {
    if (command === 'local_token') {
      assert.equal(payload?.host, 'remote.example');
      return null;
    }
    if (command === 'plugin:http|fetch') {
      const config = payload!.clientConfig as Record<string, unknown>;
      calls.push(String(config.url));
      assert.equal(new Headers(config.headers as [string, string][]).get('authorization'), 'Bearer paired-token');
      return 1;
    }
    if (command === 'plugin:http|fetch_send') {
      return { status: 200, statusText: 'OK', url: 'https://remote.example/agents', headers: [['content-type', 'application/json']], rid: 2 };
    }
    if (command === 'plugin:http|fetch_read_body') {
      if (read) return [1];
      read = true;
      return [...new TextEncoder().encode('[]'), 0];
    }
    throw new Error(`Unexpected IPC command: ${command}`);
  });
  assert.deepEqual(await new APIClient('https://remote.example').agents(), []);
  assert.deepEqual(calls, ['https://remote.example/agents']);
});

test('native requests carry auth, multipart files and a no-redirect policy', async () => {
  const requests: Record<string, unknown>[] = [];
  const attachment = { id: 'file-1', name: 'notes.txt', size: 5, mime: 'text/plain' };
  mockIPC((command, payload) => {
    if (command === 'plugin:http|fetch') {
      requests.push(payload!.clientConfig as Record<string, unknown>);
      return 1;
    }
    if (command === 'plugin:http|fetch_send') {
      return { status: 200, statusText: 'OK', url: 'http://localhost/attachments', headers: [['content-type', 'application/json']], rid: 2 };
    }
    if (command === 'plugin:http|fetch_read_body') {
      if (requests.at(-1)!.read) return [1];
      requests.at(-1)!.read = true;
      return [...new TextEncoder().encode(JSON.stringify(attachment)), 0];
    }
    throw new Error(`Unexpected IPC command: ${command}`);
  });
  const client = new APIClient('http://localhost/', 'test-token');
  const progress: number[] = [];
  assert.deepEqual(await client.upload(new File(['hello'], 'notes.txt'), (value) => progress.push(value)), attachment);
  const config = requests[0];
  const headers = new Headers(config.headers as [string, string][]);
  assert.equal(headers.get('authorization'), 'Bearer test-token');
  assert.equal(headers.get('x-polymux-client'), 'polymux-kit');
  assert.match(headers.get('content-type')!, /^multipart\/form-data; boundary=/);
  assert.equal(headers.get('origin'), '');
  assert.equal(config.maxRedirections, 0);
  const body = new TextDecoder().decode(new Uint8Array(config.data as number[]));
  assert.match(body, /filename="notes.txt"/);
  assert.match(body, /hello/);
  assert.deepEqual(progress, [0, 1]);
});

test('native responses stream before completion and release their body when canceled', async () => {
  let reads = 0;
  let canceled = false;
  mockIPC((command) => {
    if (command === 'plugin:http|fetch') return 1;
    if (command === 'plugin:http|fetch_send') {
      return { status: 200, statusText: 'OK', url: 'http://localhost/stream', headers: [['content-type', 'text/event-stream']], rid: 2 };
    }
    if (command === 'plugin:http|fetch_read_body') {
      reads++;
      return [...new TextEncoder().encode(`data: ${reads}\n\n`), 0];
    }
    if (command === 'plugin:http|fetch_cancel_body') { canceled = true; return; }
    throw new Error(`Unexpected IPC command: ${command}`);
  });
  const response = await request('http://localhost/stream');
  const reader = response.body!.getReader();
  const chunk = await reader.read();
  assert.equal(chunk.done, false);
  assert.equal(new TextDecoder().decode(chunk.value), 'data: 1\n\n');
  await reader.cancel();
  assert.equal(canceled, true);
});

test('native cancellation stays an AbortError so callers do not retry it', async () => {
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(request('http://localhost/', { signal: controller.signal }), { name: 'AbortError' });
});

test('streamed events preserve UTF-8 characters split across chunks', async () => {
  const bytes = new TextEncoder().encode('data: {"text":"你好 👍 café"}\r\n\r\n');
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const byte of bytes) controller.enqueue(Uint8Array.of(byte));
      controller.close();
    },
  });
  const events = [];
  for await (const event of decodeSSE<{ text: string }>(body)) events.push(event);
  assert.deepEqual(events, [{ text: '你好 👍 café' }]);
});

test('silent and cancelled streams release their readers', async () => {
  for (const canceled of [false, true]) {
    let closed = false;
    const body = new ReadableStream<Uint8Array>({ cancel() { closed = true; } });
    const abort = new AbortController();
    const iterator = decodeSSE(body, abort.signal, canceled ? 1000 : 10);
    const next = iterator.next();
    if (canceled) abort.abort();
    await assert.rejects(next, { name: canceled ? 'AbortError' : 'TimeoutError' });
    assert.equal(closed, true);
  }
  let closed = false;
  const body = new ReadableStream<Uint8Array>({
    start(controller) { controller.enqueue(new TextEncoder().encode('data: 1\n\n')); },
    cancel() { closed = true; },
  });
  for await (const _ of decodeSSE(body)) break;
  assert.equal(closed, true);
});
