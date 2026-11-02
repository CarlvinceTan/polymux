import { expect, test } from '@playwright/test';

test('profile galleries classify attachments, open cached files and jump to the exact part after a reply', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const image = { id: 'image', name: 'photo.png', kind: 'image', mimeType: 'image/png', size: 68, url: '/attachments/image', indexingState: 'ready' };
  const video = { id: 'video', name: 'movie.mp4', kind: 'video', mimeType: 'video/mp4', size: 2000, url: '/attachments/video', thumbnailURL: '/attachments/poster', indexingState: 'ready' };
  const audio = { id: 'audio', name: 'voice.wav', kind: 'audio', mimeType: 'audio/wav', size: 500, url: '/attachments/audio', indexingState: 'ready' };
  const file = { id: 'file', name: 'notes.txt', kind: 'file', mimeType: 'text/plain', size: 11, url: '/attachments/file', indexingState: 'ready' };
  const messages = [
    { id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1, createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text', text: 'Caption' }, { kind: 'attachment', attachment: image }, { kind: 'attachment', attachment: video }, { kind: 'attachment', attachment: audio }, { kind: 'attachment', attachment: file }], status: 'done', reactions: [] },
    { id: 'm2', threadID: 'bot', role: 'user', seq: 2, createdAt: '2026-01-01T00:00:01Z', parts: [{ kind: 'text', text: 'Reply' }], reply: { messageID: 'm1', role: 'assistant', agentID: 'bot', text: 'Caption', partIndex: 0 }, status: 'done', reactions: [] },
  ];
  let offline = false;
  const reads: string[] = [];
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.startsWith('/attachments/')) {
      reads.push(path);
      if (offline) return route.abort();
      if (path === '/attachments/file') return route.fulfill({ contentType: 'text/plain', body: 'Hello world' });
      return route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO7+8x0AAAAASUVORK5CYII=', 'base64') });
    }
    const data = path === '/agents' ? [agent]
      : path === '/agents/bot' ? { agent, prompt: '' }
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 2, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? messages
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Reply to Bot: Caption' }).click();
  await expect(page.locator('[data-message-id="m1"] [data-part-index="0"]')).toHaveClass(/highlighted/);
  await page.locator('main button.chat-header').click();
  await page.getByRole('tab', { name: 'Media' }).click();
  const gallery = page.getByLabel('Media gallery');
  await expect(gallery.getByRole('button', { name: 'Show photo.png in chat' }).locator('img')).toBeVisible();
  await expect(gallery.getByRole('button', { name: 'Show movie.mp4 in chat' }).locator('img')).toBeVisible();
  await expect(gallery.getByRole('button', { name: 'View voice.wav' })).toBeVisible();
  await expect(gallery.getByRole('button', { name: 'View notes.txt' })).toHaveCount(0);
  await gallery.getByRole('button', { name: 'Show movie.mp4 in chat' }).click();
  await expect(page.locator('[data-message-id="m1"] [data-part-index="2"]')).toHaveClass(/highlighted/);
  await page.getByRole('tab', { name: 'Files' }).click();
  await page.getByLabel('Files list').getByRole('button', { name: 'View notes.txt' }).click();
  await expect(page.getByRole('dialog', { name: 'notes.txt' })).toContainText('Hello world');
  await page.getByRole('button', { name: 'Close preview' }).click();
  offline = true;
  const before = reads.filter((path) => ['/attachments/file', '/attachments/poster', '/attachments/image'].includes(path)).length;
  await page.reload();
  await expect(page.locator('main button.chat-header')).toBeVisible();
  await expect(page.locator('[data-message-id="m1"] img[alt="photo.png"]')).toBeVisible();
  if (!await page.getByRole('tab', { name: 'Files' }).isVisible()) await page.locator('main button.chat-header').click();
  await page.getByRole('tab', { name: 'Files' }).click();
  await page.getByLabel('Files list').getByRole('button', { name: 'View notes.txt' }).click();
  await expect(page.getByRole('dialog', { name: 'notes.txt' })).toContainText('Hello world');
  await page.getByRole('button', { name: 'Close preview' }).click();
  await page.getByRole('tab', { name: 'Media' }).click();
  await expect(gallery.getByRole('button', { name: 'Show photo.png in chat' }).locator('img')).toBeVisible();
  await expect(gallery.getByRole('button', { name: 'Show movie.mp4 in chat' }).locator('img')).toBeVisible();
  expect(reads.filter((path) => ['/attachments/file', '/attachments/poster', '/attachments/image'].includes(path))).toHaveLength(before);
});

test('transcript and gallery share an in-flight image when the gallery closes', async ({ page }) => {
  const image = { id: 'image', name: 'held.png', kind: 'image', mimeType: 'image/png', size: 68, url: '/attachments/held', indexingState: 'ready' };
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const message = { id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1, createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'attachment', attachment: image }], status: 'done', reactions: [] };
  let count = 0;
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/attachments/held') {
      count++;
      await held;
      return route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO7+8x0AAAAASUVORK5CYII=', 'base64') });
    }
    const data = path === '/agents' ? [agent] : path === '/agents/bot' ? { agent, prompt: '' }
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? [message]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  try {
    await page.goto('/');
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
    await expect.poll(() => count).toBe(1);
    await page.locator('main button.chat-header').click();
    await page.getByRole('tab', { name: 'Media' }).click();
    await expect(page.getByRole('button', { name: 'Show held.png in chat' })).toBeVisible();
    await page.getByRole('button', { name: 'Close side panel' }).click();
    release();
    await expect(page.locator('[data-message-id="m1"] img[alt="held.png"]')).toBeVisible();
    expect(count).toBe(1);
  } finally { release(); }
});

test('shared media requests abort only after the last subscriber leaves and can restart', async ({ page }) => {
  await page.goto('/avatar-test.html');
  const result = await page.evaluate(async () => {
    const { mediaFile } = await import('/lib/shell/mediaCache.ts');
    let calls = 0;
    let aborts = 0;
    const client = { attachment: (_url: string, signal: AbortSignal) => {
      calls++;
      return new Promise<Blob>((_resolve, reject) => signal.addEventListener('abort', () => { aborts++; reject(new DOMException('Aborted', 'AbortError')); }, { once: true }));
    } };
    const first = new AbortController();
    const second = new AbortController();
    const a = mediaFile('cancel-test', '/shared', client, first.signal).catch((error: Error) => error.name);
    const b = mediaFile('cancel-test', '/shared', client, second.signal).catch((error: Error) => error.name);
    while (!calls) await new Promise((resolve) => setTimeout(resolve, 10));
    first.abort();
    const one = await a;
    const middle = aborts;
    second.abort();
    const two = await b;
    const third = new AbortController();
    const c = mediaFile('cancel-test', '/shared', client, third.signal).catch((error: Error) => error.name);
    while (calls < 2) await new Promise((resolve) => setTimeout(resolve, 10));
    third.abort();
    return { calls, aborts, middle, one, two, three: await c };
  });
  expect(result).toEqual({ calls: 2, aborts: 2, middle: 0, one: 'AbortError', two: 'AbortError', three: 'AbortError' });
});

test('existing media remains available when the cache separates its usage metadata', async ({ page }) => {
  await page.goto('/avatar-test.html');
  const result = await page.evaluate(async () => {
    const key = JSON.stringify(['legacy-server', '/image']);
    const old = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('polymux-media', 1);
      request.onupgradeneeded = () => request.result.createObjectStore('files');
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    await new Promise<void>((resolve, reject) => {
      const tx = old.transaction('files', 'readwrite');
      tx.objectStore('files').put({ bytes: new TextEncoder().encode('cached').buffer, type: 'text/plain', used: 1 }, key);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
    old.close();
    const { mediaFile } = await import('/lib/shell/mediaCache.ts');
    const client = { attachment: async () => { throw new Error('Unexpected network request'); } };
    const blob = await mediaFile('legacy-server', '/image', client);
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('polymux-media', 2);
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    const saved = await new Promise<{ file: { used: number }; meta: { used: number; size: number } }>((resolve) => {
      const tx = db.transaction(['files', 'metadata']);
      const file = tx.objectStore('files').get(key);
      const meta = tx.objectStore('metadata').get(key);
      tx.oncomplete = () => resolve({ file: file.result, meta: meta.result });
    });
    db.close();
    return { text: await blob.text(), oldUsed: saved.file.used, newUsed: saved.meta.used, size: saved.meta.size };
  });
  expect(result.text).toBe('cached');
  expect(result.oldUsed).toBe(1);
  expect(result.newUsed).toBeGreaterThan(1);
  expect(result.size).toBe(6);
});

test('media eviction retains a new oversized file while removing older entries', async ({ page }) => {
  await page.goto('/avatar-test.html');
  const result = await page.evaluate(async () => {
    const { mediaFile } = await import('/lib/shell/mediaCache.ts');
    const limit = 200 * 1024 * 1024;
    let requests = 0;
    const client = { attachment: async (url: string) => {
      requests++;
      const blob = new Blob([url], { type: 'text/plain' });
      // Exercise size boundaries without allocating hundreds of MiB in the browser.
      Object.defineProperty(blob, 'size', { value: url === '/near' ? limit - 1 : url === '/small' ? 1 : limit + 1 });
      return blob;
    } };
    const keys = async () => {
      const db = await new Promise<IDBDatabase>((resolve, reject) => {
        const request = indexedDB.open('polymux-media', 2);
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });
      const values = await new Promise<IDBValidKey[]>((resolve, reject) => {
        const request = db.transaction('metadata').objectStore('metadata').getAllKeys();
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });
      db.close();
      return values;
    };
    await mediaFile('size-test', '/near', client);
    await mediaFile('size-test', '/small', client);
    const atLimit = await keys();
    await mediaFile('size-test', '/large', client);
    const overLimit = await keys();
    const cached = await mediaFile('size-test', '/large', client);
    return { atLimit, overLimit, content: await cached.text(), requests };
  });
  expect(result.atLimit).toEqual([JSON.stringify(['size-test', '/near']), JSON.stringify(['size-test', '/small'])]);
  expect(result.overLimit).toEqual([JSON.stringify(['size-test', '/large'])]);
  expect(result.content).toBe('/large');
  expect(result.requests).toBe(3);
});
