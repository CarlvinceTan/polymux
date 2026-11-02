import { expect, test } from '@playwright/test';

const agent = (id: string, name: string) => ({ id, name, role: '', model: '', active: true });

function mockNotifications(page: import('@playwright/test').Page, permission: NotificationPermission) {
  return page.addInitScript((initial) => {
    class FakeNotification {
      static permission = initial;
      static requestPermission = async () => {
        FakeNotification.permission = initial === 'denied' ? 'denied' : 'granted';
        return FakeNotification.permission;
      };
      onclick: (() => void) | null = null;
      constructor(public title: string, public options: NotificationOptions) {
        (window as unknown as { __notices: unknown[] }).__notices.push({ title, body: options.body, data: options.data });
      }
      close() {}
    }
    Object.defineProperty(window, 'Notification', { value: FakeNotification, configurable: true });
    (window as unknown as { __notices: unknown[] }).__notices = [];
  }, permission);
}

test('enabled notifications poll after the saved cursor and deliver foreground alerts', async ({ page }) => {
  await mockNotifications(page, 'default');
  const requests: (string | null)[] = [];
  let call = 0;
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/notifications') {
      const after = new URL(route.request().url()).searchParams.get('after');
      requests.push(after);
      call++;
      const now = new Date().toISOString();
      const body = call === 1
        ? { cursor: 5, notices: [{ body: 'Old', outcome: 'notice', seq: 5, conversationID: 'other', messageID: 'm5', agentID: 'other', title: 'Other', createdAt: now }] }
        : call === 2
          ? { cursor: 6, notices: [{ body: 'New message', outcome: 'notice', seq: 6, conversationID: 'other', messageID: 'm6', agentID: 'other', title: 'Other', createdAt: now }] }
          : { cursor: 6, notices: [] };
      return route.fulfill({ json: body });
    }
    const data = path === '/agents' ? [agent('bot', 'Bot'), agent('other', 'Other')]
      : path === '/threads' ? [
        { id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] },
        { id: 'other', agentID: 'other', agentIDs: ['other'], title: 'Other', lastSeq: 0, unreadCount: 0, runs: [] },
      ]
      : path === '/threads/bot/messages' || path === '/threads/other/messages' ? []
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: { name: 'Test' } }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const toggle = page.getByRole('button', { name: 'Allow notifications' });
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(() => page.evaluate(() => (window as unknown as { __notices: unknown[] }).__notices.length), { timeout: 8000 }).toBe(1);
  const notices = await page.evaluate(() => (window as unknown as { __notices: { title: string; body: string }[] }).__notices);
  expect(notices[0]).toMatchObject({ title: 'Other', body: 'New message' });
  expect(requests.slice(0, 2)).toEqual([null, '5']);
});

test('muting an agent suppresses that agent’s notifications', async ({ page }) => {
  await mockNotifications(page, 'default');
  let calls = 0;
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/notifications') {
      calls++;
      const now = new Date().toISOString();
      return route.fulfill({ json: { cursor: calls + 5, notices: calls < 2 ? [] : [{ body: 'Muted message', outcome: 'notice', seq: 9, conversationID: 'other', messageID: 'm9', agentID: 'other', title: 'Other', createdAt: now }] } });
    }
    const data = path === '/agents' ? [agent('bot', 'Bot'), agent('other', 'Other')]
      : path === '/threads' ? [
        { id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] },
        { id: 'other', agentID: 'other', agentIDs: ['other'], title: 'Other', lastSeq: 0, unreadCount: 0, runs: [] },
      ]
      : path === '/threads/bot/messages' || path === '/threads/other/messages' ? []
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: { name: 'Test' } }
      : path === '/agents/other' ? { agent: agent('other', 'Other'), prompt: '' }
      : path.endsWith('/configuration') ? { skills: [], tools: [] }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Other' }).click();
  await page.getByRole('button', { name: 'Other' }).last().click();
  const mute = page.getByRole('button', { name: 'Mute notifications' });
  await mute.click();
  await expect(page.getByRole('button', { name: 'Unmute notifications' })).toHaveAttribute('aria-pressed', 'true');
  await page.getByRole('button', { name: 'Close side panel' }).click();
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Bot' }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Allow notifications' }).click();
  await expect.poll(() => calls, { timeout: 8000 }).toBeGreaterThan(1);
  expect(await page.evaluate(() => (window as unknown as { __notices: unknown[] }).__notices.length)).toBe(0);
});

test('notification permission denial is shown without enabling alerts', async ({ page }) => {
  await mockNotifications(page, 'denied');
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const data = path === '/agents' ? [agent('bot', 'Bot')]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? []
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: { name: 'Test' } }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const toggle = page.getByRole('button', { name: 'Allow notifications' });
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-pressed', 'false');
  await expect(page.getByRole('status')).toContainText('Allow Polymux notifications');
});
