import { expect, test } from '@playwright/test';

test('search uses server categories and ignores an older response', async ({ page }) => {
  const calls: string[] = [];
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/search') {
      const query = url.searchParams.get('q')!;
      calls.push(`${query}:${url.searchParams.get('type')}`);
      if (query === 'old') await new Promise((resolve) => setTimeout(resolve, 900));
      return route.fulfill({ json: [{
        id: `message/${query}`, kind: 'messages', title: `Result ${query}`,
        snippet: `Match ${query}`, threadID: 'chat', score: 1,
      }] });
    }
    const response = url.pathname === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : url.pathname === '/settings' ? { appearance: 'system' } : [];
    return route.fulfill({ json: response });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  const search = page.getByRole('textbox', { name: 'Search everything' });
  await search.fill('old');
  await expect.poll(() => calls.includes('old:all')).toBe(true);
  await search.fill('new');
  await expect(page.getByRole('button', { name: /Result new/ })).toBeVisible();
  await expect.poll(() => calls.includes('new:all')).toBe(true);
  await page.waitForTimeout(1000);
  await expect(page.getByRole('button', { name: /Result old/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'Files', exact: true }).click();
  await expect.poll(() => calls.includes('new:files')).toBe(true);
});

test('opening a message result loads older pages and reveals that message in its chat', async ({ page }) => {
  const before: string[] = [];
  const makeMessage = (seq: number) => ({
    id: `m${seq}`, seq, role: 'user', agentID: '', threadID: 'chat', status: 'done',
    parts: [{ kind: 'text', text: `Message ${seq}` }], createdAt: '2026-01-01T00:00:00Z',
    runID: '', mentions: [], reactions: [],
  });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/search') return route.fulfill({ json: [{
      id: 'message/m30', kind: 'messages', title: 'Old result', snippet: 'Message 30',
      threadID: 'chat', messageID: 'm30', score: 1,
    }] });
    if (url.pathname === '/threads/chat/messages') {
      const cursor = url.searchParams.get('before');
      if (cursor) before.push(cursor);
      const last = cursor ? Number(cursor) - 1 : 300;
      const first = Math.max(1, last - 99);
      return route.fulfill({ json: Array.from({ length: last - first + 1 }, (_, i) => makeMessage(first + i)) });
    }
    if (url.pathname === '/threads/chat/draft') return route.fulfill({ json: { revision: 0, writeID: '', value: { text: '', mentions: [] } } });
    const body = url.pathname === '/agents' ? [{ id: 'chat', name: 'Bot', role: '', model: '', active: true }]
      : url.pathname === '/threads' ? [{ id: 'chat', agentID: 'chat', agentIDs: ['chat'], title: 'Bot', lastSeq: 300, unreadCount: 0, runs: [] }]
      : url.pathname === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : url.pathname === '/settings' ? { appearance: 'system' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.evaluate(() => {
    const scroll = Element.prototype.scrollTo;
    Element.prototype.scrollTo = function (...args: Parameters<typeof scroll>) {
      if (this.classList.contains('transcript') && typeof args[0] === 'object' && args[0].behavior === 'smooth') {
        this.dispatchEvent(new Event('scroll'));
      }
      Reflect.apply(scroll, this, args);
    };
  });
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await page.getByRole('textbox', { name: 'Search everything' }).fill('old');
  await page.getByRole('button', { name: /Old result/ }).click();
  const target = page.locator('[data-message-id="m30"]');
  await expect(target).toBeVisible();
  await expect(target.locator('.bubble')).toHaveClass(/highlighted/);
  await expect(target).toBeInViewport();
  await expect(target.locator('.bubble')).not.toHaveClass(/highlighted/, { timeout: 3000 });
  await expect(target).toBeInViewport();
  expect(before).toEqual(['201', '101']);
});

test('profile search limits results and categories to its chat', async ({ page }) => {
  let searchURL = '';
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/search') {
      searchURL = url.toString();
      return route.fulfill({ json: [{ id: 'message/m1', kind: 'messages', title: 'Found here', snippet: '', threadID: 'chat', messageID: 'm1', score: 1 }] });
    }
    if (url.pathname === '/threads/chat/draft') return route.fulfill({ json: { revision: 0, writeID: '', value: { text: '', mentions: [] } } });
    const body = url.pathname === '/agents' ? [{ id: 'chat', name: 'Bot', role: '', model: '', active: true }]
      : url.pathname === '/threads' ? [{ id: 'chat', agentID: 'chat', agentIDs: ['chat'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : url.pathname === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : url.pathname === '/settings' ? { appearance: 'system' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('.chat-header').click();
  await page.getByRole('button', { name: 'Search this chat' }).click();
  const dialog = page.getByRole('dialog', { name: 'Search' });
  await expect(dialog.getByRole('button', { name: 'Chats', exact: true })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Bots', exact: true })).toHaveCount(0);
  await dialog.getByRole('textbox', { name: 'Search this chat' }).fill('needle');
  await expect(dialog.getByRole('button', { name: /Found here/ })).toBeVisible();
  expect(new URL(searchURL).searchParams.get('thread')).toBe('chat');
  await dialog.getByRole('button', { name: 'Close search' }).click();
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Search everything' })).toBeVisible();
});
