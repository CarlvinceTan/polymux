import { expect, test } from '@playwright/test';

test('answers and reactions acknowledged during the last replay request survive reconstruction', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const choice = { id: 'choice', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', status: 'done', parts: [{ kind: 'choice', question: 'Pick one', options: [{ id: 'yes', label: 'Yes' }] }] };
  const message = { id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 2,
    createdAt: '2026-01-01T00:00:01Z', status: 'done', parts: [{ kind: 'text', text: 'A message' }], reactions: [] };
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  let replayWaiting = false;
  let offline = false;
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    if (offline) return route.abort('failed');
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === '/threads/bot/events') {
      if (Number(url.searchParams.get('after')) === 0) return route.fulfill({ json: [
        { seq: 1, conversationID: 'bot', type: 'message.done', message: choice },
        { seq: 2, conversationID: 'bot', type: 'message.done', message },
      ] });
      replayWaiting = true;
      await held;
      return route.fulfill({ json: [] });
    }
    if (url.pathname === '/threads/bot/messages/choice/answer') return route.fulfill({ json: { ...choice, parts: [{ ...choice.parts[0], answerID: 'yes' }] } });
    if (url.pathname === '/threads/bot/messages/m1/reaction') return route.fulfill({ json: { ...message, reactions: [{ emoji: '👍' }] } });
    if (url.pathname === '/threads/bot/stream') return route.fulfill({ contentType: 'text/event-stream', body: '' });
    const data = url.pathname === '/agents' ? [agent]
      : url.pathname === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 2, unreadCount: 0, runs: [] }]
      : url.pathname === '/threads/bot/messages' ? [choice, message]
      : url.pathname === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : url.pathname === '/settings' ? { appearance: 'system' }
      : url.pathname.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect.poll(() => replayWaiting).toBe(true);
  await page.locator('[data-message-id="choice"]').getByRole('button', { name: 'A. Yes' }).click();
  await page.locator('[data-message-id="m1"]').getByText('A message').click({ button: 'right' });
  await page.getByRole('menuitemcheckbox', { name: 'React 👍' }).click();
  await expect(page.getByRole('button', { name: 'Reaction 👍, 1, You' })).toBeEnabled();
  release();
  await expect.poll(() => page.evaluate(async () => {
    const { Cache } = await import('/lib/cache.ts');
    const cached = await new Cache('http://127.0.0.1:8787').chat('bot');
    return { cursor: cached?.cursor, answer: cached?.messages.find((item) => item.id === 'choice')?.parts[0].answerID,
      reaction: cached?.messages.find((item) => item.id === 'm1')?.reactions?.[0]?.emoji };
  })).toEqual({ cursor: 2, answer: 'yes', reaction: '👍' });
  offline = true;
  await page.reload();
  await expect(page.locator('[data-message-id="choice"]').getByText('Yes', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Reaction 👍, 1, You' })).toBeVisible();
});

test('accepted outbox cleanup waits for replay and preserves a newer reaction', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const other = { id: 'other', name: 'Other', role: '', model: '', active: true };
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  let replayWaiting = false;
  let sent: { id: string; threadID: string; role: string; seq: number; createdAt: string; status: string; parts: { kind: string; text: string }[]; reactions: { emoji: string }[] } | undefined;
  let writes = 0;
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/bot/events') { replayWaiting = true; await held; return route.fulfill({ json: [] }); }
    if (path === '/threads/bot/stream') return route.fulfill({ contentType: 'text/event-stream', body: '' });
    if (path === '/threads/bot/messages' && request.method() === 'POST') {
      writes++;
      sent = { id: request.postDataJSON().id, threadID: 'bot', role: 'user', seq: 1, createdAt: '2026-01-01T00:00:00Z', status: 'done', parts: [{ kind: 'text', text: 'Sent while replay waits' }], reactions: [] };
      return route.fulfill({ json: sent });
    }
    if (path.endsWith('/reaction')) return route.fulfill({ json: { ...sent, reactions: [{ emoji: '👍' }] } });
    const data = path === '/agents' ? [agent, other]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] },
        { id: 'other', agentID: 'other', agentIDs: ['other'], title: 'Other', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect.poll(() => replayWaiting).toBe(true);
  await page.getByRole('textbox', { name: 'Message' }).fill('Sent while replay waits');
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect.poll(() => writes).toBe(1);
  const row = page.locator('[data-message-id]').filter({ hasText: 'Sent while replay waits' });
  await expect(row.getByRole('button', { name: 'Sent while replay waits' })).toBeVisible();
  await row.getByText('Sent while replay waits').click({ button: 'right' });
  await page.getByRole('menuitemcheckbox', { name: 'React 👍' }).click();
  await expect(page.getByRole('button', { name: 'Reaction 👍, 1, You' })).toBeEnabled();
  const entries = () => page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => { const request = indexedDB.open('polymux-outbox'); request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error); });
    return await new Promise<number>((resolve) => { const request = db.transaction('entries').objectStore('entries').count(); request.onsuccess = () => { db.close(); resolve(request.result); }; });
  });
  await expect.poll(entries).toBe(1);
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Other' }).click();
  release();
  expect(await entries()).toBe(1);
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: /Bot/ }).click();
  await expect.poll(entries, { timeout: 5000 }).toBe(0);
  expect(writes).toBe(1);
  await expect(page.getByRole('button', { name: 'Reaction 👍, 1, You' })).toBeVisible();
});
