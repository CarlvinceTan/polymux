import { expect, test } from '@playwright/test';

test('voice conversation streams a spoken reply and accepts typed fallback', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const writes: unknown[] = [];
  await page.addInitScript(() => {
    Object.defineProperty(window, 'speechSynthesis', { value: { cancel() {}, speak(utterance: SpeechSynthesisUtterance) { utterance.onend?.(new Event('end') as SpeechSynthesisEvent); } } });
  });
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/bot/voice/stream') {
      writes.push(request.postDataJSON());
      return route.fulfill({ contentType: 'text/event-stream', body: `data: ${JSON.stringify({ text: 'Hello from voice', agentID: 'bot' })}\n\n` });
    }
    const data = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? []
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system', voiceMode: 'pipeline' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Speech conversation' }).click();
  const panel = page.getByRole('dialog', { name: 'Voice conversation' });
  await expect(panel).toBeVisible();
  await panel.getByRole('textbox', { name: 'Voice message', exact: true }).fill('Hello');
  await panel.getByRole('button', { name: 'Send voice message' }).click();
  await expect(panel.getByText('Hello from voice')).toBeVisible();
  expect(writes).toEqual([{ id: expect.any(String), text: 'Hello' }]);
  await panel.getByRole('button', { name: 'Close voice' }).click();
  await expect(panel).toHaveCount(0);
});
