import { expect, test } from '@playwright/test';

test('voice settings expose live conversation options and persist changes', async ({ page }) => {
  let settings = {
    appearance: 'system', defaultProvider: '', defaultModel: '', defaultReasoning: 'off',
    voiceMode: 'pipeline', liveProvider: 'gemini', liveModel: 'gemini-3.8-live', liveVoice: 'Aoede',
    voiceLanguage: '', speechKeys: ['gemini'],
  };
  const writes: Record<string, unknown>[] = [];
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/settings' && request.method() === 'PATCH') {
      const patch = request.postDataJSON();
      writes.push(patch);
      settings = { ...settings, ...patch };
      return route.fulfill({ json: settings });
    }
    const data = path === '/settings' ? settings
      : path === '/providers' ? []
      : path === '/providers/options' ? []
      : path === '/network' ? { devices: [] }
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Models', exact: true }).click();
  const mode = page.getByLabel('Voice mode', { exact: true });
  await expect(mode).toHaveValue('pipeline');
  await expect(page.getByText('Speak, then wait for a reply.')).toBeVisible();
  await mode.selectOption('live');
  await expect(page.getByLabel('Voice provider', { exact: true })).toHaveValue('gemini');
  await expect(page.getByLabel('Voice model', { exact: true })).toHaveValue('gemini-3.8-live');
  expect(writes[0]).toEqual({ voiceMode: 'live' });

  await page.getByLabel('Voice provider', { exact: true }).selectOption('openai');
  await expect(page.getByLabel('Voice model', { exact: true })).toHaveValue('gpt-realtime');
  expect(writes.at(-1)).toEqual({ liveProvider: 'openai', liveModel: 'gpt-realtime', liveVoice: 'marin' });
  await expect(page.getByText('No OpenAI provider yet.')).toBeVisible();

  await expect(page.getByLabel('Voice language', { exact: true })).toBeEnabled();
  await page.getByLabel('Voice language', { exact: true }).selectOption('ja');
  await expect.poll(() => writes.at(-1)).toEqual({ voiceLanguage: 'ja' });
  await page.getByRole('button', { name: 'Close settings' }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Models', exact: true }).click();
  await expect(page.getByLabel('Voice mode', { exact: true })).toHaveValue('live');
  await expect(page.getByLabel('Voice language', { exact: true })).toHaveValue('ja');
});
