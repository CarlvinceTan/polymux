import { expect, test } from '@playwright/test';

const pixel = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO7+8x0AAAAASUVORK5CYII=', 'base64');

test('attachment preview opens and shares a browser file', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'share', { value: async (data: ShareData) => { (window as unknown as { __shared: unknown[] }).__shared.push({ title: data.title, files: (data.files ?? []).map((file) => ({ name: file.name, type: file.type })) }); }, configurable: true });
    Object.defineProperty(navigator, 'canShare', { value: () => true, configurable: true });
    Object.defineProperty(window, 'open', { value: (url: string) => { (window as unknown as { __opened: string }).__opened = url; return null; }, configurable: true });
    (window as unknown as { __shared: unknown[] }).__shared = [];
  });
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const file = { id: 'file', name: 'notes.txt', kind: 'file', mimeType: 'text/plain', size: 11, url: '/attachments/file/content', indexingState: 'ready' };
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/attachments/file/content') return route.fulfill({ contentType: 'text/plain', body: 'Hello world' });
    const data = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? [{ id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1, createdAt: '2026-01-01T00:00:00Z', status: 'done', reactions: [], parts: [{ kind: 'attachment', attachment: file }] }]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: { name: 'Test' } }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'View notes.txt' }).click();
  const dialog = page.getByRole('dialog', { name: 'notes.txt' });
  await expect(dialog).toContainText('Hello world');
  await dialog.getByRole('button', { name: 'Open' }).click();
  await expect.poll(() => page.evaluate(() => (window as unknown as { __opened: string }).__opened.startsWith('blob:'))).toBe(true);
  await dialog.getByRole('button', { name: 'Share' }).click();
  await expect.poll(() => page.evaluate(() => (window as unknown as { __shared: { title: string; files: { name: string; type: string }[] }[] }).__shared.length)).toBe(1);
  expect(await page.evaluate(() => (window as unknown as { __shared: { title: string; files: { name: string; type: string }[] }[] }).__shared[0])).toEqual({ title: 'notes.txt', files: [{ name: 'notes.txt', type: 'text/plain' }] });
});

test('transcript attachments open in one accessible preview with media and document fallbacks', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const image = { id: 'image', name: 'photo.png', kind: 'image', mimeType: 'image/png', size: 68, url: '/attachments/image/content', indexingState: 'ready' };
  const video = { id: 'video', name: 'movie.mp4', kind: 'video', mimeType: 'video/mp4', size: 2000, url: '/attachments/video/content', indexingState: 'ready' };
  const audio = { id: 'audio', name: 'voice.wav', kind: 'audio', mimeType: 'audio/wav', size: 500, url: '/attachments/audio/content', indexingState: 'ready' };
  const html = { id: 'html', name: 'card.html', kind: 'html', mimeType: 'text/html', size: 80, url: '/attachments/html/content', indexingState: 'ready' };
  const file = { id: 'file', name: 'notes.txt', kind: 'file', mimeType: 'text/plain', size: 11, url: '/attachments/file/content', indexingState: 'ready' };
  const message = {
    id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', status: 'done', reactions: [],
    parts: [image, video, audio, html, file].map((attachment) => ({ kind: 'attachment', attachment })),
  };
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/attachments/image/content') return route.fulfill({ contentType: 'image/png', body: pixel });
    if (path === '/attachments/video/content') return route.fulfill({ contentType: 'video/mp4', body: '' });
    if (path === '/attachments/audio/content') return route.fulfill({ contentType: 'audio/wav', body: '' });
    if (path === '/attachments/html/content') return route.fulfill({ contentType: 'text/html', body: '<!doctype html><p>Card content</p>' });
    if (path === '/attachments/file/content') return route.fulfill({ contentType: 'text/plain', body: 'Hello world' });
    const data = path === '/agents' ? [agent]
      : path === '/agents/bot' ? { agent, prompt: '' }
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? [message]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });

  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();

  await page.getByRole('button', { name: 'View photo.png' }).click();
  const imageDialog = page.getByRole('dialog', { name: 'photo.png' });
  await expect(imageDialog.locator('img')).toBeVisible();
  await expect(imageDialog.getByRole('link', { name: 'Save' })).toBeVisible();
  await page.getByRole('button', { name: 'Close preview' }).click();
  await expect(imageDialog).toHaveCount(0);

  await page.getByRole('button', { name: 'View movie.mp4' }).click();
  await expect(page.getByRole('dialog', { name: 'movie.mp4' }).locator('video')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog', { name: 'movie.mp4' })).toHaveCount(0);

  await page.getByRole('button', { name: 'View voice.wav' }).click();
  await expect(page.getByRole('dialog', { name: 'voice.wav' }).locator('audio')).toBeVisible();
  await page.getByRole('button', { name: 'Close preview' }).click();

  await page.getByRole('button', { name: 'View card.html' }).click();
  const card = page.getByRole('dialog', { name: 'card.html' }).frameLocator('iframe');
  await expect(card.getByText('Card content')).toBeVisible();
  await page.getByRole('button', { name: 'Close preview' }).click();

  await page.getByRole('button', { name: 'View notes.txt' }).click();
  await expect(page.getByRole('dialog', { name: 'notes.txt' })).toContainText('Hello world');
  await page.getByRole('button', { name: 'Close preview' }).click();
});
