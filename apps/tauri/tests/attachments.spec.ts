import { expect, test } from '@playwright/test';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';

test('file-only sends display a pending attachment and wait for its upload', async ({ page }) => {
  let finish!: () => void;
  const held = new Promise<void>((resolve) => { finish = resolve; });
  const uploaded: string[] = [];
  const sent: { text: string; attachmentIDs: string[] }[] = [];
  // WebKit's route.postData omits file bytes from multipart bodies, so observe
  // the actual loopback request instead of Playwright's intercepted copy.
  const server = createServer(async (request, response) => {
    response.setHeader('Access-Control-Allow-Origin', '*');
    response.setHeader('Access-Control-Allow-Methods', 'GET, POST, PUT, OPTIONS');
    response.setHeader('Access-Control-Allow-Headers', 'Content-Type, X-Polymux-Client, Authorization');
    if (request.method === 'OPTIONS') { response.writeHead(204).end(); return; }
    const pathname = new URL(request.url!, 'http://localhost').pathname;
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const payload = Buffer.concat(chunks).toString();
    const respond = (body: unknown) => { response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify(body)); };
    if (pathname === '/attachments' && request.method === 'POST') {
      uploaded.push(payload);
      await held;
      respond({ id: 'file-1', kind: 'file', name: 'notes.txt', mimeType: 'text/plain', size: 5, url: '/attachments/file-1' });
      return;
    }
    if (pathname === '/threads/chat/messages' && request.method === 'POST') {
      const value = JSON.parse(payload);
      sent.push(value);
      respond({ id: value.id, threadID: 'chat', role: 'user', parts: [{ kind: 'attachment', attachment: { id: 'file-1', kind: 'file', name: 'notes.txt', mimeType: 'text/plain', size: 5, url: '/attachments/file-1' } }], seq: 1, createdAt: new Date().toISOString(), status: 'done', agentID: '', runID: '', mentions: [], reactions: [] });
      return;
    }
    if (pathname === '/threads/chat/draft') { respond({ revision: 0, writeID: '', value: { text: '', mentions: [] } }); return; }
    const body = pathname === '/agents' ? [{ id: 'chat', name: 'Bot', role: '', model: '', active: true }]
      : pathname === '/threads' ? [{ id: 'chat', agentID: 'chat', agentIDs: ['chat'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : pathname === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : pathname === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : pathname === '/settings' ? { appearance: 'system' } : [];
    respond(body);
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  try {
    await page.goto('/');
    await page.getByRole('textbox', { name: 'Server address' }).fill(address);
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
    await page.getByRole('button', { name: 'Add attachment' }).click();
    const [chooser] = await Promise.all([
      page.waitForEvent('filechooser'),
      page.getByRole('menuitem', { name: 'Files' }).click(),
    ]);
    await chooser.setFiles([
      { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') },
      { name: 'remove.txt', mimeType: 'text/plain', buffer: Buffer.from('remove') },
    ]);
    await expect(page.getByRole('button', { name: 'Remove notes.txt' })).toHaveCount(1);
    await page.getByRole('button', { name: 'Remove remove.txt' }).click({ force: true });
    await expect(page.getByRole('button', { name: 'Remove remove.txt' })).toHaveCount(0);
    await page.getByRole('button', { name: 'Send message' }).click();
    await expect.poll(() => uploaded.length).toBe(1);
    await expect(page.getByLabel('notes.txt')).toBeVisible();
    expect(sent).toHaveLength(0);
    expect(uploaded[0]).toContain('hello');
    finish();
    await expect.poll(() => sent.length).toBe(1);
    expect(sent[0].text).toBe('');
    expect(sent[0].attachmentIDs).toEqual(['file-1']);
  } finally {
    finish();
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});

test('camera denial is shown without affecting the draft', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'mediaDevices', { value: {
      getUserMedia: async () => { throw new DOMException('denied', 'NotAllowedError'); },
    } });
  });
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = path === '/agents' ? [{ id: 'chat', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'chat', agentID: 'chat', agentIDs: ['chat'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('textbox', { name: 'Message' }).fill('Draft');
  await page.getByRole('button', { name: 'Add attachment' }).click();
  await page.getByRole('menuitem', { name: 'Camera' }).click();
  await expect(page.getByText('Camera access is off.')).toBeVisible();
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('Draft');
});

test('native HTTP serialization preserves bytes from a selected file', async ({ page }) => {
  await page.goto('/avatar-test.html');
  await page.evaluate(() => {
    const input = document.createElement('input');
    input.type = 'file';
    input.id = 'selected-file';
    document.body.appendChild(input);
  });
  await page.locator('#selected-file').setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') });
  const payload = await page.evaluate(async () => {
    const element = document.querySelector<HTMLInputElement>('#selected-file')!;
    const file = element.files![0];
    element.value = '';
    let body = '';
    let reads = 0;
    const result = JSON.stringify({ id: 'file-1', kind: 'file', name: file.name, size: file.size, mimeType: file.type, url: '/attachments/file-1' });
    Object.assign(window, { __TAURI_INTERNALS__: {
      invoke: async (command: string, args: { clientConfig?: { data?: number[] } }) => {
        if (command === 'plugin:http|fetch') { body = new TextDecoder().decode(new Uint8Array(args.clientConfig!.data!)); return 1; }
        if (command === 'plugin:http|fetch_send') return { status: 200, statusText: 'OK', url: 'http://localhost/attachments', headers: [['content-type', 'application/json']], rid: 2 };
        if (command === 'plugin:http|fetch_read_body') return reads++ ? [1] : [...new TextEncoder().encode(result), 0];
        throw new Error(`Unexpected IPC command: ${command}`);
      },
    } });
    const { APIClient } = await import('/lib/api/client.ts');
    await new APIClient('http://localhost/', 'test-token').upload(file);
    return body;
  });
  expect(payload).toContain('filename="notes.txt"');
  expect(payload).toContain('hello');
});

test('attachments remain with their chat and a failed upload keeps the pending file visible', async ({ page }) => {
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/attachments') return route.fulfill({ status: 500, json: { message: 'upload failed' } });
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
  await page.getByRole('button', { name: 'Add attachment' }).click();
  const [chooser] = await Promise.all([page.waitForEvent('filechooser'), page.getByRole('menuitem', { name: 'Files' }).click()]);
  await chooser.setFiles({ name: 'keep.txt', mimeType: 'text/plain', buffer: Buffer.from('keep') });
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'second' }).click();
  await expect(page.getByRole('button', { name: 'Remove keep.txt' })).toHaveCount(0);
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'first' }).click();
  await expect(page.getByRole('button', { name: 'Remove keep.txt' })).toHaveCount(1);
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(page.getByText('Not sent', { exact: false })).toBeVisible();
  await expect(page.getByLabel('keep.txt')).toBeVisible();
});

test('a camera capture adds a photo and stops the camera when the panel closes', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'mediaDevices', { value: {
      getUserMedia: async () => {
        const canvas = document.createElement('canvas');
        canvas.width = 64;
        canvas.height = 48;
        const stream = canvas.captureStream(5);
        const track = stream.getVideoTracks()[0];
        const paint = () => {
          if (track.readyState === 'ended') return;
          canvas.getContext('2d')!.fillRect(0, 0, 64, 48);
          requestAnimationFrame(paint);
        };
        paint();
        Object.assign(window, { __cameraTrack: track });
        return stream;
      },
    } });
  });
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = path === '/agents' ? [{ id: 'chat', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'chat', agentID: 'chat', agentIDs: ['chat'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Add attachment' }).click();
  await page.getByRole('menuitem', { name: 'Camera' }).click();
  const take = page.getByRole('button', { name: 'Take photo' });
  await expect(take).toBeEnabled();
  await take.click();
  await expect(page.getByRole('button', { name: 'Remove Photo.jpg' })).toHaveCount(1);
  await expect.poll(() => page.evaluate(() => (window as unknown as { __cameraTrack: MediaStreamTrack }).__cameraTrack.readyState)).toBe('ended');
});

test('camera becomes ready when video dimensions arrive after loaded data', async ({ page }) => {
  await page.addInitScript(() => {
    let dimensions = false;
    for (const key of ['videoWidth', 'videoHeight'] as const) {
      const original = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, key)!;
      Object.defineProperty(HTMLVideoElement.prototype, key, { configurable: true, get() { return dimensions ? original.get!.call(this) : 0; } });
    }
    document.addEventListener('loadeddata', (event) => {
      if (event.target instanceof HTMLVideoElement) Object.assign(window, { cameraLoaded: true });
    }, true);
    Object.assign(window, { showDimensions: () => {
      dimensions = true;
      document.querySelector('video')?.dispatchEvent(new Event('resize'));
    } });
    Object.defineProperty(navigator, 'mediaDevices', { value: {
      getUserMedia: async () => {
        const canvas = document.createElement('canvas');
        canvas.width = 64;
        canvas.height = 48;
        const stream = canvas.captureStream(5);
        const track = stream.getVideoTracks()[0];
        const paint = () => {
          if (track.readyState === 'ended') return;
          canvas.getContext('2d')!.fillRect(0, 0, 64, 48);
          requestAnimationFrame(paint);
        };
        paint();
        return stream;
      },
    } });
  });
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = path === '/agents' ? [{ id: 'chat', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'chat', agentID: 'chat', agentIDs: ['chat'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Add attachment' }).click();
  await page.getByRole('menuitem', { name: 'Camera' }).click();
  await expect.poll(() => page.evaluate(() => (window as unknown as { cameraLoaded?: boolean }).cameraLoaded)).toBe(true);
  await expect(page.getByRole('button', { name: 'Take photo' })).toBeDisabled();
  await page.evaluate(() => (window as unknown as { showDimensions: () => void }).showDimensions());
  await expect(page.getByRole('button', { name: 'Take photo' })).toBeEnabled();
  await page.getByRole('button', { name: 'Take photo' }).click();
  await expect(page.getByRole('button', { name: 'Remove Photo.jpg' })).toBeVisible();
});
