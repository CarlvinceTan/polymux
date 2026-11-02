import { expect, test } from '@playwright/test';

test('question cards support options, custom answers, dismissal and failed writes', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const card = (id: string) => ({ id, threadID: 'bot', role: 'assistant', agentID: 'bot', seq: Number(id.slice(1)),
    createdAt: '2026-01-01T00:00:00Z', status: 'done', parts: [{ kind: 'choice', question: `Question ${id}`,
      options: [{ id: 'one', label: 'Plan my day' }, { id: 'two', label: 'Build something' }], allowText: true }] });
  const messages = [card('m1'), card('m2'), card('m3')];
  const writes: { id: string; input: { optionID: string; text?: string } }[] = [];
  let offline = false;
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    if (offline) return route.abort('failed');
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const match = path.match(/^\/threads\/bot\/messages\/(m\d)\/answer$/);
    if (match) {
      const input = request.postDataJSON();
      writes.push({ id: match[1], input });
      if (writes.length === 1) return route.fulfill({ status: 503, json: { code: 'unavailable', message: 'offline' } });
      if (match[1] === 'm1') await held;
      const message = messages.find((item) => item.id === match[1])!;
      Object.assign(message.parts[0], { answerID: input.optionID, answerText: input.text });
      return route.fulfill({ json: message });
    }
    if (path === '/threads/bot/stream') return route.fulfill({ contentType: 'text/event-stream', body: '' });
    const data = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 3, lastMessage: messages[2], unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? messages
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByText('Question m3')).toBeVisible();
  const first = page.locator('[data-message-id="m1"]');
  await expect(first.getByText('Question m1')).toBeVisible();
  await first.getByRole('group', { name: 'Question: Question m1' }).focus();
  await page.keyboard.press('Shift+F10');
  await expect(page.getByRole('menu', { name: 'Reactions' })).toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Copy', exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await first.getByRole('button', { name: 'A. Plan my day' }).click();
  await expect(first.getByText('Couldn’t record that choice.')).toBeVisible();
  await first.getByRole('button', { name: 'B. Build something' }).click();
  await expect(first.getByText('Build something', { exact: true })).toBeVisible();
  await expect(first.getByRole('button', { name: 'Dismiss question' })).toHaveCount(0);
  release();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1]).toEqual({ id: 'm1', input: { optionID: 'two' } });

  const second = page.locator('[data-message-id="m2"]');
  await expect(second.getByRole('button', { name: 'Send', exact: true })).toBeDisabled();
  await second.getByRole('textbox', { name: 'Another answer' }).fill('  Custom answer  ');
  await second.getByRole('textbox', { name: 'Another answer' }).press('Enter');
  await expect(second.getByText('Custom answer', { exact: true })).toBeVisible();
  await expect.poll(() => writes.length).toBe(3);
  expect(writes[2]).toEqual({ id: 'm2', input: { optionID: '_text', text: 'Custom answer' } });

  const third = page.locator('[data-message-id="m3"]');
  await third.getByRole('button', { name: 'Dismiss question' }).click();
  await expect(third.getByText('Skipped')).toBeVisible();
  await expect.poll(() => writes.length).toBe(4);
  expect(writes[3]).toEqual({ id: 'm3', input: { optionID: '_dismissed' } });
  await expect.poll(() => page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('polymux-cache');
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    return await new Promise<unknown>((resolve) => {
      const request = db.transaction('snapshots').objectStore('snapshots').get(JSON.stringify(['http://127.0.0.1:8787', 'chat/bot']));
      request.onsuccess = () => { db.close(); resolve(request.result?.map((message: { parts: { answerID?: string }[] }) => message.parts[0].answerID)); };
      request.onerror = () => { db.close(); resolve(null); };
    });
  })).toEqual(['two', '_text', '_dismissed']);
  offline = true;
  await page.reload();
  await expect(page.locator('[data-message-id="m1"]').getByText('Build something', { exact: true })).toBeVisible();
  await expect(page.locator('[data-message-id="m2"]').getByText('Custom answer', { exact: true })).toBeVisible();
  await expect(page.locator('[data-message-id="m3"]').getByText('Skipped')).toBeVisible();
});
