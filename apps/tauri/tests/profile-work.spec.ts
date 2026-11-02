import { expect, test } from '@playwright/test';

test('profile work shows cached routines and delegated tasks, and restores a failed cancellation', async ({ page }) => {
  let fail = true;
  let offline = false;
  let routines = [{ id: 'routine', agentID: 'bot', prompt: 'Check reports', status: 'pending', at: '', intervalSeconds: 0, createdAt: '', trigger: 'time' }];
  const runs = [
    { id: 'done', agentID: 'bot', trigger: 'child', status: 'completed', activity: '', summary: 'Finished task', createdAt: '2026-01-02T00:00:00Z' },
    { id: 'active', agentID: 'bot', trigger: 'child', status: 'running', activity: 'Preparing', summary: 'Active task', createdAt: '2026-01-01T00:00:00Z' },
    { id: 'other', agentID: 'bot', trigger: 'message', status: 'running', activity: 'Ignored', summary: 'Ignored', createdAt: '2026-01-03T00:00:00Z' },
  ];
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    if (offline) return route.abort('failed');
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/agents/bot/routines/routine' && request.method() === 'DELETE') {
      if (fail) { fail = false; return route.fulfill({ status: 503, json: { message: 'unavailable' } }); }
      routines = [];
      return route.fulfill({ json: { deleted: true } });
    }
    const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
    const body = path === '/agents/bot/routines' ? routines
      : path === '/agents/bot' ? { agent, prompt: '' }
      : path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  await expect(page.getByText('Check reports')).toBeVisible();
  await expect(page.getByText('Waiting for your approval')).toBeVisible();
  const active = page.getByText('Preparing', { exact: true });
  const completed = page.getByText('Finished task', { exact: true });
  await expect(active).toBeVisible();
  expect(await active.evaluate((node, other) => Boolean(node.compareDocumentPosition(other) & Node.DOCUMENT_POSITION_FOLLOWING), await completed.elementHandle())).toBe(true);
  await expect(page.getByText('Ignored', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Cancel Check reports' }).click();
  await expect(page.getByRole('status')).toHaveText('Couldn’t cancel that routine.');
  await expect(page.getByText('Check reports')).toBeVisible();
  await page.getByRole('button', { name: 'Cancel Check reports' }).click();
  await expect(page.getByText('No scheduled work')).toBeVisible();
  await expect.poll(async () => page.evaluate(async () => {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('polymux-cache');
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
    return await new Promise<unknown>((resolve) => {
      const request = db.transaction('snapshots').objectStore('snapshots').get(JSON.stringify(['http://127.0.0.1:8787', 'routines/bot']));
      request.onsuccess = () => { db.close(); resolve(request.result); };
      request.onerror = () => { db.close(); resolve(null); };
    });
  })).toEqual([]);
  offline = true;
  await page.reload();
  await expect(page.getByText('No scheduled work')).toBeVisible();
});
