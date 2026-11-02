import { expect, test } from '@playwright/test';

test('message actions copy and share the selected text part', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (text: string) => { (window as any).copied = text; } } });
    Object.defineProperty(navigator, 'share', { configurable: true, value: async (data: ShareData) => { (window as any).shared = data; } });
  });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
    const message = { id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1, createdAt: '2026-01-01T00:00:00Z',
      parts: [{ kind: 'text', text: 'First part' }, { kind: 'text', text: 'Second Ω part' }], status: 'done', reactions: [] };
    const body = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? [message]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const part = page.locator('[data-message-id="m1"] [data-part-index="1"]');
  await part.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Copy', exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).copied)).toBe('Second Ω part');
  await part.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Share', exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).shared)).toEqual({ text: 'Second Ω part' });
});

test('copy failure is reported and unavailable sharing stays visible', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async () => { throw new Error('blocked'); } } });
    Object.defineProperty(navigator, 'share', { configurable: true, value: undefined });
    document.execCommand = () => false;
  });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = path === '/agents' ? [{ id: 'bot', name: 'Bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? [{ id: 'm1', threadID: 'bot', role: 'user', seq: 1, createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text', text: 'Hello' }], status: 'done', reactions: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByText('Hello').click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Share', exact: true })).toBeDisabled();
  await page.getByRole('menuitem', { name: 'Copy', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Couldn’t copy this message.' })).toBeVisible();
});

test('pending messages can be copied without enabling reactions', async ({ page }) => {
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (text: string) => { (window as any).copied = text; } } });
    Object.defineProperty(navigator, 'share', { configurable: true, value: undefined });
  });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/bot/messages' && request.method() === 'POST') {
      const sent = request.postDataJSON();
      await held;
      return route.fulfill({ json: { id: sent.id, threadID: 'bot', role: 'user', seq: 1, createdAt: '2026-01-01T00:00:00Z', parts: [{ kind: 'text', text: sent.text }], status: 'done', reactions: [] } });
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
  await page.getByRole('textbox', { name: 'Message' }).fill('Pending text');
  await page.getByRole('button', { name: 'Send message' }).click();
  const bubble = page.getByRole('button', { name: 'Pending text' });
  await expect(bubble).toBeVisible();
  await bubble.focus();
  await page.keyboard.press('Shift+F10');
  const menu = page.getByRole('menu', { name: 'Message actions' });
  await expect(menu.getByRole('menuitemcheckbox')).toHaveCount(0);
  await expect(menu.getByRole('menuitem', { name: 'Share', exact: true })).toBeDisabled();
  await menu.getByRole('menuitem', { name: 'Copy', exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).copied)).toBe('Pending text');
  await bubble.focus();
  await page.keyboard.press('Shift+F10');
  await expect(page.getByRole('menu', { name: 'Message actions' })).toBeVisible();
  release();
  const accepted = page.getByRole('menu', { name: 'Reactions' });
  await expect(accepted.getByRole('menuitemcheckbox', { name: 'React 👍' })).toBeVisible();
  await expect(accepted.getByRole('menuitem', { name: 'Reply', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(bubble).toBeFocused();
});
