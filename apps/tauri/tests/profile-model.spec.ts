import { expect, test } from '@playwright/test';

test('profile model and reasoning save per bot and roll back a failed save', async ({ page }) => {
  let agent = { id: 'bot', name: 'Bot', role: '', model: 'gpt-4', provider: 'one', reasoning: 'off', active: true };
  const writes: Record<string, unknown>[] = [];
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/agents/bot' && request.method() === 'PATCH') {
      const patch = request.postDataJSON() as Record<string, unknown>;
      writes.push(patch);
      if (patch.reasoning === 'high') return route.fulfill({ status: 500, json: { message: 'nope' } });
      agent = { ...agent, ...patch } as typeof agent;
      return route.fulfill({ json: { agent, prompt: '' } });
    }
    const data = path === '/agents' ? [agent]
      : path === '/agents/bot' ? { agent, prompt: '' }
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? []
      : path === '/providers/options' ? [{ type: 'openai', name: 'OpenAI', auth: 'api_key' }]
      : path === '/providers' ? [
        { id: 'one', type: 'openai', name: 'Work', status: 'api_key', enabled: true, models: [{ id: 'gpt-4', tags: ['text'], enabled: true }, { id: 'gpt-5-1', tags: ['text'], enabled: true }, { id: 'voice', tags: ['audio'], enabled: true }] },
      ]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: {} }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });

  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  await page.getByRole('tab', { name: 'Capabilities' }).click();

  const model = page.getByRole('button', { name: /^Model,/ });
  await expect(model).toHaveAccessibleName('Model, gpt-4 Work');
  await model.click();
  await page.getByRole('textbox', { name: 'Search models' }).fill('gpt-5');
  await page.getByRole('option', { name: 'Gpt 5.1' }).click();
  await expect(model).toHaveAccessibleName('Model, gpt-5-1 Work');
  expect(writes).toEqual([{ provider: 'one', model: 'gpt-5-1' }]);

  const reasoning = page.getByRole('button', { name: /^Reasoning,/ });
  await expect(reasoning).toHaveAccessibleName('Reasoning, Off');
  await reasoning.click();
  await page.getByRole('option', { name: 'High', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Couldn’t save model settings.');
  await expect(reasoning).toHaveAccessibleName('Reasoning, Off');

  await reasoning.click();
  await page.getByRole('option', { name: 'Medium', exact: true }).click();
  await expect(reasoning).toHaveAccessibleName('Reasoning, Medium');
  expect(writes.at(-1)).toEqual({ reasoning: 'medium' });
});
