import { expect, test } from '@playwright/test';

test('dictation preserves the draft, finalizes on pause, and stops on chat switch', async ({ page }) => {
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const agents = ['Alpha', 'Beta'].map((name) => ({ id: name.toLowerCase(), name, role: '', model: '', active: true }));
    const body = path === '/agents' ? agents
      : path === '/threads' ? agents.map((agent) => ({ id: agent.id, agentID: agent.id, agentIDs: [agent.id], title: agent.name, lastSeq: 0, unreadCount: 0, runs: [] }))
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Alpha' }).click();
  await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();
  await page.evaluate(async () => {
    const { mockSpeech } = await import('/dictation-test.ts');
    const speech = mockSpeech();
    (window as any).speechCalls = speech.calls;
    (window as any).speech = speech.emit;
  });
  const emit = (event: object) => page.evaluate((payload) => (window as any).speech(payload), { id: 1, ...event });
  await page.getByRole('button', { name: 'Start dictation' }).click();
  await expect(page.getByRole('button', { name: 'Cancel dictation' })).toBeVisible();
  await emit({ kind: 'ready' });
  await expect(page.getByRole('img', { name: 'Recording' })).toBeVisible();
  await emit({ kind: 'transcript', text: 'Hello', start: 0, final: false });
  await emit({ kind: 'transcript', text: 'Hello there', start: 0, final: false });
  await page.getByRole('button', { name: 'Pause dictation' }).click();
  await emit({ kind: 'transcript', text: 'Hello there.', start: 0, final: true });
  await emit({ kind: 'ended' });
  await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('Hello there.');
  expect(await page.evaluate(() => (window as any).speechCalls)).toEqual([{ id: 1, finalize: true }]);
  await page.getByRole('textbox', { name: 'Message' }).fill('');
  await page.getByRole('button', { name: 'Start dictation' }).click();
  await emit({ kind: 'ready' });
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Beta' }).click();
  await emit({ kind: 'transcript', text: 'late', start: 0, final: true });
  await emit({ kind: 'ended' });
  await expect(page.getByRole('textbox', { name: 'Message' })).toHaveValue('');
  expect(await page.evaluate(() => (window as any).speechCalls)).toEqual([{ id: 1, finalize: true }, { id: 1, finalize: false }]);
});
