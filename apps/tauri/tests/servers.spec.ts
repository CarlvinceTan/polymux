import { expect, test } from '@playwright/test';

test('settings switch between remembered servers and remember the last one', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('polymux.servers', JSON.stringify([
      { address: 'http://127.0.0.1:8787', name: 'Alpha' },
      { address: 'http://127.0.0.1:8788', name: 'Bravo' },
    ]));
  });
  const server = (name: string) => {
    const id = name.toLowerCase();
    return {
      agent: { id, name, role: '', model: '', active: true },
      thread: { id, agentID: id, agentIDs: [id], title: name, lastSeq: 0, unreadCount: 0, runs: [] },
    };
  };
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const value = server('Alpha');
    const data = path === '/agents' ? [value.agent] : path === '/threads' ? [value.thread]
      : path === '/threads/alpha/messages' ? []
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path === '/network' ? { server: { name: 'Alpha' }, host: 'host', current: 'device', devices: [], online: [] }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.route('http://127.0.0.1:8788/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const value = server('Bravo');
    const data = path === '/agents' ? [value.agent] : path === '/threads' ? [value.thread]
      : path === '/threads/bravo/messages' ? []
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path === '/network' ? { server: { name: 'Bravo' }, host: 'host', current: 'device', devices: [], online: [] }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Alpha' })).toBeVisible();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByLabel('Server', { exact: true }).selectOption('http://127.0.0.1:8788');
  await expect(page.getByRole('button', { name: 'Close settings' })).toHaveCount(0);
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Bravo' })).toBeVisible();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('polymux.server'))).toBe('http://127.0.0.1:8788');
});
