import { expect, test } from '@playwright/test';

test('dropping files on a chat attaches them and shows the drop target', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const data = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? []
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.locator('.pane')).toBeVisible();
  await page.evaluate(() => {
    const transfer = new DataTransfer();
    transfer.items.add(new File(['hello'], 'drop.txt', { type: 'text/plain' }));
    const pane = document.querySelector('.pane')!;
    pane.dispatchEvent(new DragEvent('dragenter', { bubbles: true, cancelable: true, dataTransfer: transfer }));
  });
  await expect(page.getByText('Drop files to attach')).toBeVisible();
  await page.evaluate(() => {
    const transfer = new DataTransfer();
    transfer.items.add(new File(['hello'], 'drop.txt', { type: 'text/plain' }));
    const pane = document.querySelector('.pane')!;
    pane.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: transfer }));
  });
  await expect(page.getByText('Drop files to attach')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Remove drop.txt' })).toBeVisible();
});
