import { expect, test } from '@playwright/test';

test('reactions toggle optimistically, preserve newer events, and roll back failures', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const message = { id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text', text: 'Hello there' }], status: 'done',
    reactions: [{ emoji: '👍', agentID: 'bot' }] };
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  const writes: { emoji: string; remove: boolean }[] = [];
  let emit = false;
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/bot/events') {
      const after = Number(new URL(request.url()).searchParams.get('after'));
      return route.fulfill({ json: after === 0 ? [{ seq: 1, conversationID: 'bot', type: 'message.done', message }] : [] });
    }
    if (path === '/threads/bot/messages/m1/reaction') {
      const input = request.postDataJSON();
      writes.push(input);
      if (writes.length === 1) {
        await held;
        return route.fulfill({ json: { ...message, reactions: [...message.reactions, { emoji: '👍' }] } });
      }
      if (input.emoji === '❤️') return route.fulfill({ status: 503, json: { code: 'unavailable', message: 'offline' } });
      return route.fulfill({ json: { ...message, reactions: [{ emoji: '👍', agentID: 'bot' }, { emoji: '😂', agentID: 'bot' }] } });
    }
    if (path === '/threads/bot/stream') {
      if (emit) {
        emit = false;
        const event = { seq: 2, conversationID: 'bot', type: 'message.reactions', messageID: 'm1',
          message: { ...message, reactions: [...message.reactions, { emoji: '👍' }, { emoji: '😂', agentID: 'bot' }] } };
        return route.fulfill({ contentType: 'text/event-stream', body: `data: ${JSON.stringify(event)}\n\n` });
      }
      return route.fulfill({ contentType: 'text/event-stream', body: '' });
    }
    const data = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? [message]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const row = page.locator('[data-message-id="m1"]');
  await expect(row.getByRole('button', { name: 'Reaction 👍, 1, Bot' })).toBeVisible();
  await row.getByRole('button', { name: 'Hello there' }).focus();
  await page.keyboard.press('Shift+F10');
  await expect(page.getByRole('menu', { name: 'Reactions' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu', { name: 'Reactions' })).toHaveCount(0);
  await expect(row.getByRole('button', { name: 'Hello there' })).toBeFocused();
  await row.getByText('Hello there').click({ button: 'right' });
  await page.getByRole('menuitemcheckbox', { name: 'React 👍' }).click();
  await expect(row.getByRole('button', { name: /Reaction 👍, 2/ })).toHaveAttribute('aria-pressed', 'true');
  await expect(row.getByRole('button', { name: /Reaction 👍, 2/ })).toBeDisabled();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).toEqual({ emoji: '👍', remove: false });
  await row.getByText('Hello there').click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Copy', exact: true })).toBeEnabled();
  await expect(page.getByRole('menuitemcheckbox', { name: 'React 👍' })).toBeDisabled();
  await page.keyboard.press('Escape');
  emit = true;
  await expect(row.getByRole('button', { name: 'Reaction 😂, 1, Bot' })).toBeVisible({ timeout: 5000 });
  release();
  await expect(row.getByRole('button', { name: 'Reaction 😂, 1, Bot' })).toBeVisible();
  await row.getByText('Hello there').click({ button: 'right' });
  await page.getByRole('menuitemcheckbox', { name: 'React ❤️' }).click();
  await expect(row.getByText('Couldn’t update this reaction.')).toBeVisible();
  await expect(row.getByRole('button', { name: /Reaction ❤️/ })).toHaveCount(0);
  await row.getByRole('button', { name: /Reaction 👍, 2/ }).click();
  await expect.poll(() => writes.length).toBe(3);
  expect(writes[2]).toEqual({ emoji: '👍', remove: true });
  await expect(row.getByRole('button', { name: 'Reaction 👍, 1, Bot' })).toHaveAttribute('aria-pressed', 'false');
  await expect(row.getByText('Couldn’t update this reaction.')).toHaveCount(0);
  await expect.poll(() => page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('polymux-cache');
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    return await new Promise<unknown>((resolve) => {
      const request = db.transaction('snapshots').objectStore('snapshots').get(JSON.stringify(['http://127.0.0.1:8787', 'chat/bot']));
      request.onsuccess = () => { db.close(); resolve(request.result?.[0]?.reactions); };
      request.onerror = () => { db.close(); resolve(null); };
    });
  })).toEqual([{ emoji: '👍', agentID: 'bot' }, { emoji: '😂', agentID: 'bot' }]);
});
