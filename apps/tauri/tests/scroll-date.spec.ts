import { expect, test } from '@playwright/test';

test('scrolling the transcript shows the date of the first visible message', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const messages = Array.from({ length: 30 }, (_, index) => ({
    id: `m${index}`, threadID: 'bot', role: index % 2 ? 'assistant' : 'user', agentID: index % 2 ? 'bot' : undefined,
    seq: index + 1, createdAt: new Date(Date.UTC(2026, 0, index < 15 ? 1 : 2, 12, index)).toISOString(),
    status: 'done', reactions: [], parts: [{ kind: 'text', text: `Message ${index}` }],
  }));
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const data = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 30, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? messages
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.locator('[data-message-id="m29"]')).toBeVisible();
  await page.locator('.transcript').evaluate((element) => {
    element.scrollTop = 0;
    element.dispatchEvent(new Event('scroll'));
  });
  await expect(page.locator('.scroll-date')).toBeVisible();
  await expect(page.locator('.scroll-date')).toContainText('01/01');
});
