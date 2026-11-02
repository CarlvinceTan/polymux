import { expect, test } from '@playwright/test';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';

test('a queued attachment and identical message request survive a reload', async ({ page }) => {
  const uploads: string[] = [];
  const requests: Record<string, unknown>[] = [];
  let accept = false;
  let host = 'host';
  let networkChecks = 0;
  const server = createServer(async (request, response) => {
    response.setHeader('Access-Control-Allow-Origin', '*');
    response.setHeader('Access-Control-Allow-Methods', 'GET, POST, PUT, OPTIONS');
    response.setHeader('Access-Control-Allow-Headers', 'Content-Type, X-Polymux-Client, Authorization');
    if (request.method === 'OPTIONS') { response.writeHead(204).end(); return; }
    const path = new URL(request.url!, 'http://localhost').pathname;
    if (path === '/network') networkChecks++;
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const payload = Buffer.concat(chunks).toString();
    const respond = (body: unknown) => { response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify(body)); };
    if (path === '/attachments' && request.method === 'POST') {
      uploads.push(payload);
      respond({ id: 'file-1', kind: 'file', name: 'notes.txt', mimeType: 'text/plain', size: 5, url: '/attachments/file-1' });
      return;
    }
    if (path === '/threads/chat/messages' && request.method === 'POST') {
      const value = JSON.parse(payload);
      requests.push(value);
      if (!accept) { response.statusCode = 503; respond({ message: 'offline' }); return; }
      respond({ id: value.id, threadID: 'chat', role: 'user', parts: [{ kind: 'text', text: value.text }], seq: 1, createdAt: new Date().toISOString(), status: 'done', agentID: '', runID: '', mentions: [], reactions: [] });
      return;
    }
    if (path === '/threads/chat/draft') { respond({ revision: 0, writeID: '', value: { text: '', mentions: [] } }); return; }
    const body = path === '/agents' ? [{ id: 'chat', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'chat', agentID: 'chat', agentIDs: ['chat'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host, current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' } : [];
    respond(body);
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  try {
    await page.goto('/');
    await page.getByRole('textbox', { name: 'Server address' }).fill(address);
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
    await page.getByRole('button', { name: 'Add attachment' }).click();
    const [chooser] = await Promise.all([page.waitForEvent('filechooser'), page.getByRole('menuitem', { name: 'Files' }).click()]);
    await chooser.setFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') });
    await expect(page.getByRole('button', { name: 'Remove notes.txt' })).toBeVisible();
    await expect.poll(() => page.evaluate(async () => {
      const db = await new Promise<IDBDatabase>((resolve, reject) => { const request = indexedDB.open('polymux-outbox'); request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error); });
      return await new Promise<number>((resolve) => { const request = db.transaction('drafts').objectStore('drafts').getAll(); request.onsuccess = () => resolve(request.result.flat().length); });
    })).toBe(1);
    await page.reload();
    await expect(page.getByRole('button', { name: 'Remove notes.txt' })).toBeVisible();
    await page.getByRole('textbox', { name: 'Message' }).fill('Queued message');
    await page.getByRole('button', { name: 'Send message' }).click();
    await expect(page.getByText('Not sent', { exact: false })).toBeVisible();
    expect(uploads).toHaveLength(1);
    expect(uploads[0]).toContain('hello');
    expect(requests).toHaveLength(1);
    host = 'replacement';
    await page.reload();
    await expect(page.getByText('Queued message')).toBeVisible();
    await expect(page.getByLabel('notes.txt')).toBeVisible();
    await expect(page.getByText('The server changed. Your message is kept on this device.')).toBeVisible();
    expect(requests).toHaveLength(1);
    host = '';
    const checks = networkChecks;
    await expect.poll(() => networkChecks, { timeout: 10000 }).toBeGreaterThan(checks + 1);
    expect(requests).toHaveLength(1);
    await expect(page.getByText('Waiting to verify the server before sending. Your message is kept on this device.')).toBeVisible();
    host = 'host';
    accept = true;
    await expect.poll(() => requests.length, { timeout: 10000 }).toBe(2);
    expect(requests[1]).toEqual(requests[0]);
    expect(uploads).toHaveLength(1);
    await expect.poll(() => page.evaluate(async () => {
      const db = await new Promise<IDBDatabase>((resolve, reject) => { const request = indexedDB.open('polymux-outbox'); request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error); });
      return await new Promise<number>((resolve) => { const request = db.transaction('entries').objectStore('entries').count(); request.onsuccess = () => resolve(request.result); });
    })).toBe(0);
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});

test('a chat switch during handoff clears only the submitted chat draft', async ({ page }) => {
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/first/messages' && request.method() === 'POST') {
      const body = request.postDataJSON();
      return route.fulfill({ json: { id: body.id, threadID: 'first', role: 'user', seq: 1, createdAt: new Date().toISOString(), status: 'done', agentID: '', runID: '', mentions: [], reactions: [], parts: [{ kind: 'text', text: body.text }] } });
    }
    const ids = ['first', 'second'];
    const body = path === '/agents' ? ids.map((id) => ({ id, name: id, role: '', model: '', active: true }))
      : path === '/threads' ? ids.map((id) => ({ id, agentID: id, agentIDs: [id], title: id, lastSeq: 0, unreadCount: 0, runs: [] }))
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.evaluate(async () => {
    const { Outbox } = await import('/lib/outbox.ts');
    const enqueue = Outbox.prototype.enqueue;
    const wait = new Promise<void>((resolve) => { Object.assign(window, { releaseOutbox: resolve }); });
    Outbox.prototype.enqueue = async function (...args: Parameters<typeof enqueue>) { await wait; return enqueue.apply(this, args); };
  });
  const input = page.getByRole('textbox', { name: 'Message' });
  await input.fill('First draft');
  await page.getByRole('button', { name: 'Send message' }).click();
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'second' }).click();
  await input.fill('Second draft');
  await page.evaluate(() => (window as unknown as { releaseOutbox: () => void }).releaseOutbox());
  await expect(input).toHaveValue('Second draft');
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'first' }).click();
  await expect(input).toHaveValue('');
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'second' }).click();
  await expect(input).toHaveValue('Second draft');
});

test('the composer keeps edits made while a reply is being saved', async ({ page }) => {
  const original = { id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1, createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text', text: 'Original' }, { kind: 'text', text: 'Another part' }], status: 'done', reactions: [] };
  const sent: Record<string, unknown>[] = [];
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/bot/messages' && request.method() === 'POST') {
      const body = request.postDataJSON();
      sent.push(body);
      return route.fulfill({ json: { ...original, id: body.id, role: 'user', seq: 2, parts: [{ kind: 'text', text: body.text }] } });
    }
    const body = path === '/agents' ? [{ id: 'bot', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? [original]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.evaluate(async () => {
    const { Outbox } = await import('/lib/outbox.ts');
    const enqueue = Outbox.prototype.enqueue;
    const wait = new Promise<void>((resolve) => { Object.assign(window, { releaseOutbox: resolve }); });
    Outbox.prototype.enqueue = async function (...args: Parameters<typeof enqueue>) { await wait; return enqueue.apply(this, args); };
  });
  await page.locator('[data-message-id="m1"] [data-part-index="0"]').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Reply', exact: true }).click();
  const input = page.getByRole('textbox', { name: 'Message' });
  await input.fill('First reply');
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(page.getByRole('button', { name: 'Cancel reply' })).toBeVisible();
  await input.fill('Next draft');
  await page.locator('[data-message-id="m1"] [data-part-index="1"]').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Reply', exact: true }).click();
  await page.evaluate(() => (window as unknown as { releaseOutbox: () => void }).releaseOutbox());
  await expect.poll(() => sent.length).toBe(1);
  expect(sent[0]).toMatchObject({ text: 'First reply', replyToID: 'm1', replyPart: 0 });
  await expect(input).toHaveValue('Next draft');
  await expect(page.getByLabel('Reply to Bot: Another part')).toBeVisible();
});

test('a failed local handoff retains the draft and does not send', async ({ page }) => {
  let sends = 0;
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/bot/messages' && request.method() === 'POST') sends++;
    const body = path === '/agents' ? [{ id: 'bot', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.evaluate(async () => {
    const { Outbox } = await import('/lib/outbox.ts');
    Outbox.prototype.enqueue = async () => { throw new Error('Storage full'); };
  });
  await page.getByRole('button', { name: 'Add attachment' }).click();
  const [chooser] = await Promise.all([page.waitForEvent('filechooser'), page.getByRole('menuitem', { name: 'Files' }).click()]);
  await chooser.setFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') });
  const input = page.getByRole('textbox', { name: 'Message' });
  await input.fill('Keep this draft');
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(page.getByText('Couldn’t save this message on this device. Your draft and attachments are kept here.')).toBeVisible();
  await expect(input).toHaveValue('Keep this draft');
  await expect(page.getByRole('button', { name: 'Remove notes.txt' })).toBeVisible();
  expect(sends).toBe(0);
});

test('permanent failures remain visible after reload without blocking later messages', async ({ page }) => {
  const requests: string[] = [];
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/bot/messages' && request.method() === 'POST') {
      const body = request.postDataJSON();
      requests.push(body.text);
      if (body.text === 'Rejected') return route.fulfill({ status: 400, json: { message: 'rejected' } });
      return route.fulfill({ json: { id: body.id, threadID: 'bot', role: 'user', seq: 1, createdAt: new Date().toISOString(), status: 'done', agentID: '', runID: '', mentions: [], reactions: [], parts: [{ kind: 'text', text: body.text }] } });
    }
    const body = path === '/agents' ? [{ id: 'bot', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const input = page.getByRole('textbox', { name: 'Message' });
  await input.fill('Rejected');
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(page.getByText('Not sent', { exact: false })).toBeVisible();
  await page.reload();
  await expect(page.getByText('Rejected')).toBeVisible();
  await expect(page.getByText('Not sent', { exact: false })).toBeVisible();
  await input.fill('Next message');
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect.poll(() => requests).toEqual(['Rejected', 'Next message']);
  await expect(page.getByText('Next message')).toBeVisible();
});

test('deleting an unsent attachment removes its durable queue entry in both windows', async ({ page, context }) => {
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  let uploads = 0;
  let sends = 0;
  await context.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/attachments' && request.method() === 'POST') {
      uploads++;
      await held;
      return route.fulfill({ status: 400, json: { message: 'rejected' } });
    }
    if (path === '/threads/bot/messages' && request.method() === 'POST') sends++;
    const body = path === '/agents' ? [{ id: 'bot', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  try {
    await page.goto('/');
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
    await page.getByRole('button', { name: 'Add attachment' }).click();
    const [chooser] = await Promise.all([page.waitForEvent('filechooser'), page.getByRole('menuitem', { name: 'Files' }).click()]);
    await chooser.setFiles({ name: 'remove.txt', mimeType: 'text/plain', buffer: Buffer.from('discard bytes') });
    await page.getByRole('textbox', { name: 'Message' }).fill('Discard me');
    await page.getByRole('button', { name: 'Send message' }).click();
    await expect.poll(() => uploads).toBe(1);
    const id = await page.locator('[data-message-id]').last().getAttribute('data-message-id');
    const second = await context.newPage();
    await second.goto('/');
    await expect(second.getByText('Discard me')).toBeVisible();
    await second.evaluate(async () => {
      const { Outbox } = await import('/lib/outbox.ts');
      const original = Outbox.prototype.ifMissing;
      Outbox.prototype.ifMissing = async function (...args: Parameters<typeof original>) {
        await original.apply(this, args);
        Object.assign(window, { checkedDiscard: true });
      };
    });
    await page.evaluate(async (id) => {
      const db = await new Promise<IDBDatabase>((resolve, reject) => { const request = indexedDB.open('polymux-outbox'); request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error); });
      await new Promise<void>((resolve, reject) => {
        const tx = db.transaction('entries', 'readwrite');
        const store = tx.objectStore('entries');
        const key = JSON.stringify(['http://127.0.0.1:8787', id]);
        const request = store.get(key);
        request.onsuccess = () => { store.delete(key); store.put(request.result, key); };
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error);
      });
    }, id);
    await page.evaluate((id) => {
      const channel = new BroadcastChannel(JSON.stringify(['polymux-outbox', 'http://127.0.0.1:8787']));
      channel.postMessage({ threadID: 'bot', id });
      channel.close();
    }, id);
    await expect.poll(() => second.evaluate(() => (window as unknown as { checkedDiscard?: boolean }).checkedDiscard)).toBe(true);
    await expect(second.getByText('Discard me')).toBeVisible();
    await expect(page.getByText('Sending…')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Delete unsent message' })).toHaveCount(0);
    release();
    const discard = page.getByRole('button', { name: 'Delete unsent message' });
    await expect(discard).toBeEnabled();
    await page.evaluate(async () => {
      const { Outbox } = await import('/lib/outbox.ts');
      Object.assign(window, { originalDiscard: Outbox.prototype.discard });
      Outbox.prototype.discard = async () => { throw new Error('Storage unavailable'); };
    });
    await discard.click();
    await expect(page.getByText('Couldn’t delete this message.', { exact: true })).toBeVisible();
    await expect(page.getByText('Discard me')).toBeVisible();
    await page.evaluate(async () => {
      const { Outbox } = await import('/lib/outbox.ts');
      Outbox.prototype.discard = (window as unknown as { originalDiscard: typeof Outbox.prototype.discard }).originalDiscard;
    });
    await expect(discard).toBeEnabled();
    await discard.click();
    await expect(page.getByText('Discard me')).toHaveCount(0);
    await expect(second.getByText('Discard me')).toHaveCount(0);
    await second.close();
    await page.reload();
    await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();
    await expect(page.getByText('Discard me')).toHaveCount(0);
    await expect(page.getByLabel('remove.txt')).toHaveCount(0);
    expect(uploads).toBe(1);
    expect(sends).toBe(0);
    const count = await page.evaluate(async () => {
      const db = await new Promise<IDBDatabase>((resolve, reject) => { const request = indexedDB.open('polymux-outbox'); request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error); });
      return await new Promise<number>((resolve) => { const request = db.transaction('entries').objectStore('entries').count(); request.onsuccess = () => resolve(request.result); });
    });
    expect(count).toBe(0);
  } finally { release(); }
});

test('another window cannot delete a message while its upload is in flight', async ({ page, context }) => {
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  let uploads = 0;
  let sends = 0;
  await context.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/attachments' && request.method() === 'POST') {
      uploads++;
      await held;
      return route.fulfill({ json: { id: 'file-1', kind: 'file', name: 'held.txt', mimeType: 'text/plain', size: 4, url: '/attachments/file-1' } });
    }
    if (path === '/threads/bot/messages' && request.method() === 'POST') {
      sends++;
      const body = request.postDataJSON();
      return route.fulfill({ json: { id: body.id, threadID: 'bot', role: 'user', seq: 1, createdAt: new Date().toISOString(), status: 'done', agentID: '', runID: '', mentions: [], reactions: [], parts: [{ kind: 'text', text: body.text }] } });
    }
    const body = path === '/agents' ? [{ id: 'bot', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  try {
    await page.goto('/');
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
    await page.getByRole('button', { name: 'Add attachment' }).click();
    const [chooser] = await Promise.all([page.waitForEvent('filechooser'), page.getByRole('menuitem', { name: 'Files' }).click()]);
    await chooser.setFiles({ name: 'held.txt', mimeType: 'text/plain', buffer: Buffer.from('held') });
    await page.getByRole('textbox', { name: 'Message' }).fill('Active upload');
    await page.getByRole('button', { name: 'Send message' }).click();
    await expect.poll(() => uploads).toBe(1);
    const localID = (await page.locator('[data-message-id]').last().getAttribute('data-message-id'))!;
    const second = await context.newPage();
    await second.goto('/');
    await expect(second.getByText('Active upload')).toBeVisible();
    await second.getByRole('button', { name: 'Delete unsent message' }).click();
    await expect(second.getByText('Couldn’t delete this message while it is being sent.')).toBeVisible();
    await expect(second.getByText('Active upload')).toBeVisible();
    release();
    await expect.poll(() => sends).toBe(1);
    expect(uploads).toBe(1);
    const confirmedID = localID.slice(6).replace(/-/g, '');
    await expect(page.locator(`[data-message-id="${confirmedID}"]`)).toBeVisible();
    await page.evaluate(async () => {
      const { Outbox } = await import('/lib/outbox.ts');
      const original = Outbox.prototype.ifMissing;
      Outbox.prototype.ifMissing = async function (...args: Parameters<typeof original>) {
        await original.apply(this, args);
        Object.assign(window, { checkedDiscard: true });
      };
    });
    await second.evaluate((id) => {
      const channel = new BroadcastChannel(JSON.stringify(['polymux-outbox', 'http://127.0.0.1:8787']));
      channel.postMessage({ threadID: 'bot', id });
      channel.close();
    }, localID);
    await expect.poll(() => page.evaluate(() => (window as unknown as { checkedDiscard?: boolean }).checkedDiscard)).toBe(true);
    await expect(page.locator(`[data-message-id="${confirmedID}"]`)).toBeVisible();
    await second.close();
  } finally { release(); }
});
