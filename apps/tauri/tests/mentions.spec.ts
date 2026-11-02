import { expect, test } from '@playwright/test';

test('a selected duplicate name survives draft sync, offline queueing and retry', async ({ page }) => {
  const agents = [{ id: 'one123', name: 'Alex', role: 'Alpha', model: '', active: true },
    { id: 'two456', name: 'Alex', role: 'Beta', model: '', active: true }];
  const thread = { id: 'group', agentID: 'one123', agentIDs: agents.map((agent) => agent.id), title: 'Group', lastSeq: 0, unreadCount: 0, runs: [] };
  let draft = { revision: 0, writeID: '', value: { text: '', mentions: [] as { id: string; text: string; location: number; length: number }[] } };
  let accept = false;
  const requests: { id: string; text: string; mentions?: string[] }[] = [];
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/group/draft') {
      if (request.method() === 'PUT') draft = { ...request.postDataJSON(), revision: draft.revision + 1 };
      return route.fulfill({ json: draft });
    }
    if (path === '/threads/group/messages' && request.method() === 'POST') {
      const input = request.postDataJSON();
      requests.push(input);
      if (!accept) return route.fulfill({ status: 503, json: { code: 'offline', message: 'offline' } });
      return route.fulfill({ json: { id: input.id, threadID: 'group', role: 'user', seq: 1, createdAt: '2026-01-01T00:00:00Z', status: 'done', parts: [{ kind: 'text', text: input.text }], mentions: input.mentions } });
    }
    if (path === '/threads/group/stream') return route.fulfill({ contentType: 'text/event-stream', body: '' });
    const data = path === '/agents' ? agents : path === '/threads' ? [thread]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const input = page.getByRole('textbox', { name: 'Message' });
  await input.fill('@al');
  await expect(page.getByRole('button', { name: 'Mention Alex, Alpha, one123' })).toBeVisible();
  await page.getByRole('button', { name: 'Mention Alex, Beta, two456' }).click();
  await expect(input).toHaveValue('@Alex ');
  await input.evaluate((field: HTMLTextAreaElement) => field.setSelectionRange(0, 0));
  await page.keyboard.insertText('😀 ');
  await input.evaluate((field: HTMLTextAreaElement) => field.setSelectionRange(field.value.length, field.value.length));
  await input.pressSequentially('review');
  await expect.poll(() => draft.value.mentions).toEqual([{ id: 'two456', text: '@Alex', location: 3, length: 5 }]);
  await page.reload();
  await expect(input).toHaveValue('😀 @Alex review');
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect.poll(() => requests.length).toBeGreaterThan(0);
  expect(requests[0].mentions).toEqual(['two456']);
  const first = requests[0];
  await page.reload();
  await expect(page.getByText('😀 @Alex review')).toBeVisible();
  accept = true;
  await expect.poll(() => requests.length, { timeout: 15000 }).toBeGreaterThan(1);
  expect(requests.at(-1)).toEqual(first);
});

test('everyone uses text routing and a removed member blocks a selected mention', async ({ page }) => {
  const agents = [{ id: 'one', name: 'Alex', role: '', model: '', active: true },
    { id: 'two', name: 'Bea', role: '', model: '', active: true }];
  let thread = { id: 'group', agentID: 'one', agentIDs: ['one', 'two'], title: 'Group', lastSeq: 0, unreadCount: 0, runs: [] };
  let draft = { revision: 0, writeID: '', value: { text: '', mentions: [] } };
  const requests: { mentions?: string[]; text: string }[] = [];
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/group/draft') {
      if (request.method() === 'PUT') draft = { ...request.postDataJSON(), revision: draft.revision + 1 };
      return route.fulfill({ json: draft });
    }
    if (path === '/threads/group/messages' && request.method() === 'POST') {
      const input = request.postDataJSON(); requests.push(input);
      return route.fulfill({ json: { id: input.id, threadID: 'group', role: 'user', seq: 1, createdAt: '2026-01-01T00:00:00Z', status: 'done', parts: [{ kind: 'text', text: input.text }] } });
    }
    if (path === '/threads/group/stream') return route.fulfill({ contentType: 'text/event-stream', body: '' });
    const data = path === '/agents' ? agents : path === '/threads' ? [thread]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const input = page.getByRole('textbox', { name: 'Message' });
  await input.fill('@');
  await expect(page.getByRole('button', { name: 'Mention everyone' })).toBeVisible();
  await input.press('Enter');
  await expect(input).toHaveValue('@everyone ');
  await input.pressSequentially('hello');
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect.poll(() => requests.length).toBe(1);
  expect(requests[0].text).toBe('@everyone hello');
  expect(requests[0].mentions ?? []).toEqual([]);

  await input.fill('@bea');
  await input.press('Enter');
  await expect(input).toHaveValue('@Bea ');
  thread = { ...thread, agentIDs: ['one'] };
  await expect.poll(async () => page.evaluate(() => {
    const stored = Object.keys(localStorage).find((key) => key.startsWith('polymux.draft.') && key.endsWith('.group'));
    return stored ? JSON.parse(localStorage.getItem(stored)!).state.value.mentions[0]?.id : undefined;
  })).toBe('two');
  await page.waitForTimeout(4000);
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'A mentioned agent left this chat.' })).toBeVisible();
  expect(requests).toHaveLength(1);
  await expect(input).toHaveValue('@Bea ');
});
