import { expect, test } from '@playwright/test';

test('replay repairs a stream gap and persists the cursor with its messages and runs', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const message = (text: string) => ({ id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text', text }], status: 'done' });
  const run = { id: 'run', seq: 8, conversationID: 'bot', agentID: 'bot', status: 'running', createdAt: '2026-01-01T00:00:00Z' };
  const event = (seq: number, type = 'activity', extra: object = {}) => ({ seq, conversationID: 'bot', type, ...extra });
  const initial = [event(1, 'message.start', { message: message('Stale reply') }), event(2, 'message.done', { message: message('Current reply') }),
    event(3), event(4), event(5), event(6)];
  const repair = [event(7, 'message.done', { message: message('Repaired reply') }), event(8, 'run.update', { run })];
  const cursors: number[] = [];
  let gap = false;
  let offline = false;
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    if (offline) return route.abort('failed');
    const url = new URL(route.request().url());
    const after = Number(url.searchParams.get('after'));
    if (url.pathname === '/threads/bot/events') {
      if (after === 0) { await held; return route.fulfill({ json: initial }); }
      return route.fulfill({ json: gap && after === 6 ? repair : [] });
    }
    if (url.pathname === '/threads/bot/stream') {
      cursors.push(after);
      if (!gap && after === 6) {
        gap = true;
        return route.fulfill({ contentType: 'text/event-stream', body: `data: ${JSON.stringify(repair[1])}\n\n` });
      }
      const stale = event(7, 'message.done', { message: message('Stale reply') });
      return route.fulfill({ contentType: 'text/event-stream', body: `data: ${JSON.stringify(stale)}\n\n` });
    }
    const data = url.pathname === '/agents' ? [agent]
      : url.pathname === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: gap ? 8 : 6, unreadCount: 0, runs: gap ? [run] : [] }]
      : url.pathname === '/threads/bot/messages' ? [message('Current reply')]
      : url.pathname === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : url.pathname === '/settings' ? { appearance: 'system' }
      : url.pathname.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.getByText('Current reply')).toBeVisible();
  await expect(page.getByText('Stale reply')).toHaveCount(0);
  release();
  await expect(page.locator('[data-message-id="m1"]').getByText('Repaired reply')).toBeVisible({ timeout: 10000 });
  await expect.poll(() => cursors.includes(8)).toBe(true);
  await expect(page.getByText('Stale reply')).toHaveCount(0);
  await expect.poll(() => page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('polymux-cache');
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    return await new Promise<unknown>((resolve) => {
      const transaction = db.transaction('snapshots');
      const store = transaction.objectStore('snapshots');
      const state = store.get(JSON.stringify(['http://127.0.0.1:8787', 'chat-state/bot']));
      const messages = store.get(JSON.stringify(['http://127.0.0.1:8787', 'chat/bot']));
      transaction.oncomplete = () => { db.close(); resolve({ cursor: state.result?.cursor, runs: state.result?.runs?.map((item: { id: string }) => item.id), text: messages.result?.[0]?.parts?.[0]?.text }); };
    });
  })).toEqual({ cursor: 8, runs: ['run'], text: 'Repaired reply' });
  offline = true;
  await page.reload();
  await expect(page.locator('[data-message-id="m1"]').getByText('Repaired reply')).toBeVisible();
  offline = false;
  const earlier = cursors.length;
  await expect.poll(() => cursors.length, { timeout: 10000 }).toBeGreaterThan(earlier);
  expect(cursors[earlier]).toBe(8);
  await expect(page.getByText('Stale reply')).toHaveCount(0);
});

test('replay applies delta-only events to the state at the saved cursor', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const message = (text: string) => ({ id: 'm1', threadID: 'bot', role: 'assistant' as const, agentID: 'bot', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text' as const, text }], status: 'streaming' as const });
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/threads/bot/events') {
      const after = Number(url.searchParams.get('after'));
      if (after === 1) { await held; return route.fulfill({ json: [{ seq: 2, conversationID: 'bot', type: 'message.delta', messageID: 'm1', text: ' there' }] }); }
      return route.fulfill({ json: [] });
    }
    if (url.pathname === '/threads/bot/stream') return route.fulfill({ contentType: 'text/event-stream', body: '' });
    const data = url.pathname === '/agents' ? [agent]
      : url.pathname === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 2, unreadCount: 0, runs: [] }]
      : url.pathname === '/threads/bot/messages' ? [message('Hi there')]
      : url.pathname === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : url.pathname === '/settings' ? { appearance: 'system' }
      : url.pathname.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.evaluate(async (stored) => {
    const { Cache } = await import('/lib/cache.ts');
    const cache = new Cache('http://127.0.0.1:8787');
    await cache.saveChat('bot', { messages: [stored], cursor: 1, runs: [] });
  }, message('Hi'));
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const row = page.locator('[data-message-id="m1"]');
  await expect(row.getByText('Hi there', { exact: true })).toBeVisible();
  const prior = await page.evaluate(async () => {
    const { Cache } = await import('/lib/cache.ts');
    const cached = await new Cache('http://127.0.0.1:8787').chat('bot');
    return { cursor: cached?.cursor, text: cached?.messages[0]?.parts[0]?.text };
  });
  expect(prior).toEqual({ cursor: 1, text: 'Hi' });
  release();
  await expect.poll(() => page.evaluate(async () => {
    const { Cache } = await import('/lib/cache.ts');
    const cached = await new Cache('http://127.0.0.1:8787').chat('bot');
    return { cursor: cached?.cursor, text: cached?.messages[0]?.parts[0]?.text };
  })).toEqual({ cursor: 2, text: 'Hi there' });
  await expect(row.getByText('Hi there', { exact: true })).toBeVisible();
  await expect(page.getByText('Hi there there')).toHaveCount(0);
});

test('a failed cursor write rolls back its message snapshot', async ({ page }) => {
  await page.goto('/');
  const result = await page.evaluate(async () => {
    const { Cache } = await import('/lib/cache.ts');
    const cache = new Cache('http://atomic-test');
    const message = (text: string) => ({ id: 'one', threadID: 'thread', role: 'assistant' as const, seq: 1,
      createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text' as const, text }], status: 'done' as const });
    const initial = await cache.saveChat('thread', { cursor: 1, messages: [message('Before')], runs: [] });
    const original = IDBObjectStore.prototype.put;
    IDBObjectStore.prototype.put = function (value: unknown, key?: IDBValidKey) {
      if (typeof key === 'string' && key.includes('chat-state/thread')) throw new Error('forced cache failure');
      return original.call(this, value, key);
    };
    const failed = await cache.saveChat('thread', { cursor: 2, messages: [message('After')], runs: [] });
    IDBObjectStore.prototype.put = original;
    const restored = await cache.chat('thread');
    return { initial, failed, cursor: restored?.cursor, text: restored?.messages[0]?.parts[0]?.text };
  });
  expect(result).toEqual({ initial: true, failed: false, cursor: 1, text: 'Before' });
});
