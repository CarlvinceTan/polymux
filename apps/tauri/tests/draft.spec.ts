import { expect, test } from '@playwright/test';

test('typing during a draft save keeps the newer text and saves it next', async ({ page }) => {
  const requests: { revision: number; writeID: string; value: { text: string } }[] = [];
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const { pathname } = new URL(route.request().url());
    if (pathname === '/threads/chat/draft') {
      if (route.request().method() === 'PUT') {
        const state = route.request().postDataJSON();
        requests.push(state);
        if (requests.length === 1) await held;
        return route.fulfill({ json: { ...state, revision: requests.length } });
      }
      return route.fulfill({ json: { revision: 0, writeID: '', value: { text: '', mentions: [] } } });
    }
    const body = pathname === '/agents' ? [{ id: 'chat', name: 'Bot', role: '', model: '', active: true }]
      : pathname === '/threads' ? [{ id: 'chat', agentID: 'chat', agentIDs: ['chat'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : pathname === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : pathname === '/settings' ? { appearance: 'system' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const input = page.getByRole('textbox', { name: 'Message' });
  await input.fill('first');
  await expect.poll(() => requests.length).toBe(1);
  await input.fill('newer');
  release();
  await expect.poll(() => requests.length).toBe(2);
  await expect(input).toHaveValue('newer');
  expect(requests[1].value.text).toBe('newer');
  expect(requests[1].revision).toBe(1);
});
