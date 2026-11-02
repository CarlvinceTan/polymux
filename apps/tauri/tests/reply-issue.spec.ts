import { expect, test } from '@playwright/test';

test('assistant failures show the current public warning and yield to recovery', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const sent = { id: 'sent', threadID: 'bot', role: 'user', seq: 1, createdAt: '2026-01-01T00:00:20Z',
    parts: [{ kind: 'text', text: 'What next?' }], status: 'done', reactions: [] };
  const queue: object[] = [];
  let runs: object[] = [];
  let otherRuns: object[] = [];
  let offline = false;
  let nextEvent = 2;
  const error = { code: 'no_provider', message: 'No provider is set up. Add one in Settings.', detail: 'private diagnostics' };
  const run = (id: string, seq: number, status: string) => ({ id, seq, status, agentID: 'bot', conversationID: 'bot',
    createdAt: `2026-01-01T00:0${seq}:00Z`, updatedAt: `2026-01-01T00:0${seq}:00Z`, error: status === 'failed' ? error : undefined });
  const failed = (id: string, seq: number, partial = '') => ({ id, runID: `run${seq}`, threadID: 'bot', role: 'assistant',
    agentID: 'bot', seq, createdAt: `2026-01-01T00:0${seq}:00Z`, parts: partial ? [{ kind: 'text', text: partial }] : [],
    status: 'error', error, reactions: [] });
  function emitFailure(id: string, seq: number, partial = '') {
    const failure = run(`run${seq}`, seq, 'failed');
    runs = [...runs, failure];
    queue.push({ seq: nextEvent++, conversationID: 'bot', type: 'message.error', message: failed(id, seq, partial) });
    queue.push({ seq: nextEvent++, conversationID: 'bot', type: 'run.done', run: failure });
  }
  await page.route('http://127.0.0.1:8787/**', (route) => {
    if (offline) return route.abort('failed');
    const path = new URL(route.request().url()).pathname;
    if (path === '/threads/bot/events') {
      const after = Number(new URL(route.request().url()).searchParams.get('after'));
      return route.fulfill({ json: after === 0 ? [{ seq: 1, conversationID: 'bot', type: 'message.start', message: sent }] : [] });
    }
    if (path === '/threads/bot/stream') {
      const events = queue.splice(0).map((event) => `data: ${JSON.stringify(event)}\n\n`).join('');
      return route.fulfill({ contentType: 'text/event-stream', body: events });
    }
    const data = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 10, unreadCount: 0, runs },
        { id: 'other', agentID: 'bot', agentIDs: ['bot'], title: 'Other', lastSeq: 10, unreadCount: 0, runs: otherRuns }]
      : path === '/threads/bot/messages' ? [{ id: 'intro', threadID: 'bot', role: 'assistant', agentID: 'bot', seq: 0,
        createdAt: '2026-01-01T00:00:10Z', parts: [], status: 'error', error, reactions: [] }, sent]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.getByText('What next?')).toBeVisible();
  await expect(page.locator('.transcript .status')).toHaveCount(1);
  const warning = page.getByRole('status').filter({ hasText: error.message });
  emitFailure('failed2', 2);
  await expect(warning).toBeVisible();
  await expect(page.locator('[data-message-id="failed2"]')).toHaveCount(0);
  await expect(page.getByText('Message not sent.')).toHaveCount(0);
  await expect(page.getByText('private diagnostics')).toHaveCount(0);
  await warning.getByRole('button', { name: 'Dismiss warning' }).click();
  await expect(warning).toHaveCount(0);

  emitFailure('failed3', 3, 'A partial reply');
  await expect(warning).toBeVisible();
  await expect(page.getByText('A partial reply')).toBeVisible();
  otherRuns = [{ ...run('run4', 4, 'completed'), conversationID: 'other' }];
  await expect(warning).toHaveCount(0, { timeout: 10000 });

  emitFailure('failed5', 5);
  await expect(warning).toBeVisible();
  offline = true;
  await expect(warning).toHaveCount(0, { timeout: 10000 });
  offline = false;
  await expect(warning).toBeVisible({ timeout: 10000 });
});
