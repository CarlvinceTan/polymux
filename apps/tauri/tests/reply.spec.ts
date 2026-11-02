import { expect, test } from '@playwright/test';

test('part replies persist in drafts, can be cancelled, and jump back after sending', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const original = { id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text', text: 'First part' }, { kind: 'text', text: 'Second part' }], status: 'done', reactions: [] };
  let draft = { revision: 0, writeID: '', value: { text: '', mentions: [] as unknown[], reply: undefined as unknown } };
  let sent: Record<string, unknown> | undefined;
  let release!: () => void;
  const hold = new Promise<void>((resolve) => { release = resolve; });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/bot/draft') {
      if (request.method() === 'PUT') { draft = { ...request.postDataJSON(), revision: draft.revision + 1 }; }
      return route.fulfill({ json: draft });
    }
    if (path === '/threads/bot/messages') {
      if (request.method() === 'POST') {
        sent = request.postDataJSON();
        await hold;
        return route.fulfill({ json: { id: sent?.id, threadID: 'bot', role: 'user', seq: 2, createdAt: '2026-01-01T00:00:01Z', status: 'done', parts: [{ kind: 'text', text: 'My reply' }], reactions: [], replyToID: 'm1', reply: { messageID: 'm1', role: 'assistant', agentID: 'bot', text: 'Second part', partIndex: 1 } } });
      }
      return route.fulfill({ json: [original] });
    }
    const body = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const originalRow = page.locator('[data-message-id="m1"]');
  const selectReply = async () => {
    await originalRow.getByText('Second part').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Reply', exact: true }).click();
  };
  await selectReply();
  await expect(page.getByLabel('Reply to Bot: Second part')).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Message' })).toBeFocused();
  await expect.poll(() => (draft.value.reply as { partIndex?: number } | undefined)?.partIndex).toBe(1);
  await page.reload();
  await expect(page.getByLabel('Reply to Bot: Second part')).toBeVisible();
  await page.getByRole('button', { name: 'Cancel reply' }).click();
  await expect(page.getByLabel('Reply to Bot: Second part')).toHaveCount(0);
  await selectReply();
  await page.getByRole('textbox', { name: 'Message' }).fill('My reply');
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect.poll(() => sent?.replyToID).toBe('m1');
  expect(sent?.replyPart).toBe(1);
  const quoted = page.getByRole('button', { name: 'Reply to Bot: Second part' });
  await expect(quoted).toBeVisible();
  await expect(page.getByRole('button', { name: 'Cancel reply' })).toHaveCount(0);
  release();
  await quoted.click();
  await expect(originalRow.locator('[data-part-index="1"]')).toHaveClass(/highlighted/);
});

test('replying to an attachment keeps its part and thumbnail in the draft', async ({ page }) => {
  const image = { id: 'image', name: 'photo.png', kind: 'image', mimeType: 'image/png', size: 1, url: '/attachments/image' };
  const message = { id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text', text: 'Caption' }, { kind: 'attachment', attachment: image }], status: 'done', reactions: [] };
  let reply: { partIndex?: number; attachment?: { id: string } } | undefined;
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/attachments/image') return route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO7+8x0AAAAASUVORK5CYII=', 'base64') });
    if (path === '/threads/bot/draft') {
      if (route.request().method() === 'PUT') reply = route.request().postDataJSON().value.reply;
      return route.fulfill({ json: route.request().method() === 'PUT' ? { ...route.request().postDataJSON(), revision: 1 } : { revision: 0, writeID: '', value: { text: '', mentions: [] } } });
    }
    const body = path === '/agents' ? [{ id: 'bot', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? [message]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('[data-message-id="m1"] [data-part-index="1"]').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Reply', exact: true }).click();
  const preview = page.getByLabel('Reply to Bot: photo.png');
  await expect(preview).toBeVisible();
  await expect(preview.locator('img')).toBeVisible();
  await expect.poll(() => reply?.partIndex).toBe(1);
  expect(reply?.attachment?.id).toBe('image');
});
