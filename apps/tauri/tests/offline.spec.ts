import { expect, test } from '@playwright/test';

test('cached chats reopen offline and refresh after the server returns', async ({ page }) => {
  let offline = false;
  let renamed = false;
  let failures = 0;
  const calls: Record<string, number> = {};
  const agent = (id: string, name: string) => ({ id, name, role: '', model: '', active: true });
  const thread = (id: string, title: string) => ({ id, title, agentID: id, agentIDs: [id], lastSeq: 1, lastReadSeq: 0, unreadCount: 0, runs: [] });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    if (offline) { failures++; return route.abort('failed'); }
    const path = new URL(route.request().url()).pathname;
    const id = path.split('/')[2];
    if (path.endsWith('/messages') || path.endsWith('/read')) calls[path] = (calls[path] ?? 0) + 1;
    if (path.endsWith('/draft')) return route.fulfill({ json: { revision: 0, writeID: '', value: { text: '', mentions: [] } } });
    if (path.endsWith('/messages')) return route.fulfill({ json: [{
      id: `${id}-message`, threadID: id, role: 'user', agentID: '', seq: 1, createdAt: '2026-10-01T00:00:00Z',
      parts: [{ kind: 'text', text: `Saved ${id}` }], status: 'done', runID: '', mentions: [], reactions: [],
    }] });
    const body = path === '/agents' ? [agent('first', renamed ? 'First refreshed' : 'First'), agent('second', 'Second')]
      : path === '/threads' ? [thread('first', 'First'), thread('second', 'Second')]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'system' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.getByText('Saved first')).toBeVisible();
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Second' }).click();
  await expect(page.getByText('Saved second')).toBeVisible();
  await expect.poll(() => calls['/threads/second/read']).toBe(1);
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'First' }).click();
  await expect(page.getByText('Saved first')).toBeVisible();
  await expect.poll(() => calls['/threads/first/read']).toBe(2);
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Second' }).click();
  await expect(page.getByText('Saved second')).toBeVisible();
  await expect.poll(() => calls['/threads/second/read']).toBe(2);
  await page.waitForTimeout(500);
  expect(calls['/threads/first/messages']).toBe(2);
  expect(calls['/threads/second/messages']).toBe(2);
  expect(calls['/threads/first/read']).toBe(2);
  expect(calls['/threads/second/read']).toBe(2);
  await expect.poll(async () => page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const req = indexedDB.open('polymux-cache');
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    return await new Promise<boolean>((resolve) => {
      const req = db.transaction('snapshots').objectStore('snapshots').get(JSON.stringify(['http://127.0.0.1:8787', 'chat/second']));
      req.onsuccess = () => { db.close(); resolve(Array.isArray(req.result)); };
      req.onerror = () => { db.close(); resolve(false); };
    });
  })).toBe(true);
  offline = true;
  await page.reload();
  await expect(page.getByText('Saved second')).toBeVisible();
  await page.getByRole('button', { name: 'First' }).click();
  await expect(page.getByText('Saved first')).toBeVisible();
  await expect.poll(() => failures).toBeGreaterThan(2);
  const warning = page.getByRole('status').filter({ hasText: /Connecting|Can’t reach server/ });
  await expect(warning).toBeVisible({ timeout: 10000 });
  renamed = true;
  offline = false;
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'First refreshed' })).toBeVisible({ timeout: 15000 });
  await expect(warning).toHaveCount(0);
});
