import { expect, test } from '@playwright/test';

test('credential requests validate, save, skip, and stay pending after a failure', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const request = { name: 'api', label: 'API key', kind: 'api', origin: 'https://example.com', header: 'Authorization', scheme: 'Bearer', status: 'pending' };
  const message = {
    id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', status: 'done', reactions: [],
    parts: [{ kind: 'credential', credential: request }],
  };
  const writes: unknown[] = [];
  let fail = true;
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request_ = route.request();
    const path = new URL(request_.url()).pathname;
    if (path === '/threads/bot/messages/m1/credential') {
      const body = request_.postDataJSON();
      writes.push(body);
      if (fail) { fail = false; return route.fulfill({ status: 503, json: { message: 'offline' } }); }
      return route.fulfill({ json: { ...message, parts: [{ kind: 'credential', credential: { ...request, status: body.skip ? 'skipped' : 'saved' } }] } });
    }
    const data = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? [message]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const card = page.getByRole('group', { name: 'Credential request: API key' });
  await expect(card).toContainText('https://example.com');
  await card.getByRole('button', { name: 'Save' }).click();
  await expect(card.getByRole('status')).toContainText('Enter a credential, or choose Skip.');
  expect(writes).toHaveLength(0);

  await card.getByLabel('Credential value').fill('secret-value');
  await card.getByRole('button', { name: 'Save' }).click();
  await expect(card.getByRole('status')).toContainText('Couldn’t save this credential.');
  await expect(card.getByLabel('Credential value')).toHaveValue('secret-value');
  expect(writes).toEqual([{ value: 'secret-value', skip: false }]);

  await card.getByRole('button', { name: 'Skip' }).click();
  await expect(card.getByText('Skipped')).toBeVisible();
  expect(writes.at(-1)).toEqual({ value: undefined, skip: true });
});
