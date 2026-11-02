import { expect, test } from '@playwright/test';

test('private vault edit cards load saved fields and save replacements', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const card = { action: 'edit', itemID: 'item-1', title: 'Example login', kind: 'login', url: 'https://example.com', status: 'pending' };
  const message = {
    id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', status: 'done', reactions: [], parts: [{ kind: 'vault', vault: card }],
  };
  let item = { id: 'item-1', kind: 'login', title: 'Example login', url: 'https://example.com', fields: { username: '', password: '' }, codes: [], revision: 1, removed: [] as string[] };
  const writes: unknown[] = [];
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/vault/items/item-1') return route.fulfill({ json: item });
    if (path === '/vault/cards/m1/save') {
      const body = request.postDataJSON();
      writes.push(body);
      item = { ...item, ...body, revision: 2 };
      return route.fulfill({ json: item });
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
  const cardView = page.getByRole('group', { name: 'Private vault card: Example login' });
  await cardView.getByRole('button', { name: 'Open' }).click();
  await expect(cardView.getByRole('textbox', { name: 'Username' })).toHaveAttribute('placeholder', 'Saved');
  await expect(cardView.getByRole('textbox', { name: 'Password' })).toHaveAttribute('placeholder', 'Saved');
  await cardView.getByLabel('Title').fill('Updated login');
  await cardView.getByRole('textbox', { name: 'Password' }).fill('new-secret');
  await cardView.getByRole('button', { name: 'Save' }).click();
  await expect(cardView.getByText('Completed')).toBeVisible();
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({ id: 'item-1', title: 'Updated login', fields: { username: '', password: 'new-secret' }, removed: [] });
});

test('private vault import cards report added and skipped entries', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const card = { action: 'import', itemID: 'import-1', title: 'Import passwords', kind: 'login', url: '', status: 'pending' };
  const message = {
    id: 'm2', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', status: 'done', reactions: [], parts: [{ kind: 'vault', vault: card }],
  };
  let body = '';
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/vault/cards/m2/import') {
      body = request.postData() ?? '';
      return route.fulfill({ json: { added: 2, skipped: 1 } });
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
  const cardView = page.getByRole('group', { name: 'Private vault card: Import passwords' });
  await cardView.getByRole('button', { name: 'Open' }).click();
  await cardView.getByLabel('Choose vault import').setInputFiles({ name: 'passwords.csv', mimeType: 'text/csv', buffer: Buffer.from('name,url,username,password\nExample,https://example.com,me,secret\n') });
  await cardView.getByRole('button', { name: 'Import' }).click();
  await expect(cardView.getByText('Completed')).toBeVisible();
  await expect(cardView.getByText('2 imported · 1 already present')).toBeVisible();
  if (body) expect(body).toContain('Example');
});
