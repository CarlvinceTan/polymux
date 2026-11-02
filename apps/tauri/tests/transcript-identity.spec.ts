import { expect, test } from '@playwright/test';

test('group transcripts label authors and show the active bot phrase', async ({ page }) => {
  const agents = [
    { id: 'ada', name: 'Ada', role: 'Researcher', model: '', active: true },
    { id: 'bo', name: 'Bo', role: 'Writer', model: '', active: true },
  ];
  const run = {
    jobID: '', id: 'run-1', conversationID: 'group', trigger: 'message', attempt: 1, seq: 1,
    createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', activity: 'tool',
    agentID: 'ada', status: 'running', summary: 'Searching', tools: [{ id: 'tool-1', runID: 'run-1', name: 'web_search', status: 'running' }],
  };
  const message = {
    id: 'm1', threadID: 'group', role: 'assistant', agentID: 'ada', seq: 1,
    createdAt: '2026-01-01T00:00:00Z', status: 'done', reactions: [], parts: [{ kind: 'text', text: 'Here is a finding' }],
  };
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const data = path === '/agents' ? agents
      : path === '/threads' ? [{ id: 'group', agentID: 'ada', agentIDs: ['ada', 'bo'], title: 'Research', lastSeq: 1, unreadCount: 0, runs: [run] }]
      : path === '/threads/group/messages' ? [message]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const row = page.locator('[data-message-id="m1"]');
  await expect(row.getByText('Ada', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Digging around')).toBeVisible();
});

test('a bot chat names the bot that created it', async ({ page }) => {
  const maker = { id: 'maker', name: 'Maker', role: '', model: '', active: true };
  const bot = { id: 'bot', name: 'Scout', role: '', model: '', active: true, createdBy: 'maker' };
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const data = path === '/agents' ? [maker, bot]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Scout', lastSeq: 1, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? [{ id: 'm1', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 1, createdAt: '2026-01-01T00:00:00Z', status: 'done', reactions: [], parts: [{ kind: 'text', text: 'Hello' }] }]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.getByText('Created by Maker')).toBeVisible();
});
