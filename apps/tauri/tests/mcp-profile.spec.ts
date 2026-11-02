import { expect, test } from '@playwright/test';

test('profile capabilities show readable skills and connected MCP tools', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const data = path === '/agents' ? [agent]
      : path === '/agents/bot' ? { agent, prompt: '' }
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? []
      : path === '/agents/bot/configuration' ? { skills: ['/skills/research-helper/SKILL.md'], tools: ['shell'] }
      : path === '/agents/bot/mcp' ? [{ name: 'Calendar', enabled: true, hidden: false, shared: false }]
      : path === '/agents/bot/mcp/status' ? [{ name: 'Calendar', connected: true, tools: ['list_events'] }]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  await page.getByRole('tab', { name: 'Capabilities' }).click();
  await expect(page.getByText('Research Helper')).toBeVisible();
  const connection = page.locator('.connection').filter({ hasText: 'Calendar' });
  await expect(connection).toContainText('1');
  await connection.locator('summary').click();
  await expect(connection.getByText('list_events')).toBeVisible();
  await expect(page.getByText('shell', { exact: true })).toBeVisible();
});
