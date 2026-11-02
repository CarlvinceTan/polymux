import { expect, test } from '@playwright/test';

for (const colorScheme of ['light', 'dark'] as const) {
  test(`model defaults use grouped, searchable providers and roll back failures (${colorScheme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme });
    let settings = { appearance: 'system', defaultProvider: '', defaultModel: '', defaultReasoning: 'off' };
    let offline = false;
    const writes: unknown[] = [];
    await page.route('http://127.0.0.1:8787/**', async (route) => {
      const request = route.request();
      const { pathname } = new URL(request.url());
      if (offline && ['/settings', '/providers', '/providers/options'].includes(pathname)) return route.fulfill({ status: 503 });
      if (pathname === '/settings' && request.method() === 'PATCH') {
        const patch = request.postDataJSON();
        writes.push(patch);
        if (patch.defaultReasoning === 'high') return route.fulfill({ status: 500, json: {} });
        settings = { ...settings, ...patch };
        return route.fulfill({ json: settings });
      }
      const body = pathname === '/settings' ? settings
        : pathname === '/providers/options' ? [{ type: 'openai', name: 'OpenAI', auth: 'api_key' }]
          : pathname === '/providers' ? [
            { id: 'one', type: 'openai', name: 'Work', status: 'api_key', headers: { Authorization: 'secret' }, enabled: true, models: [{ id: 'gpt-5-1', tags: ['text'], enabled: true }, { id: 'voice', tags: ['audio'], enabled: true }] },
            { id: 'two', type: 'openai', name: 'Home', status: 'api_key', enabled: true, models: [{ id: 'gpt-5-1', tags: ['text'], enabled: true }, { id: 'disabled', tags: ['text'], enabled: false }] },
            { id: 'three', type: 'anthropic', name: 'Anthropic', status: '', enabled: true, models: [{ id: 'unconnected', tags: ['text'], enabled: true }] },
          ]
            : pathname === '/network' ? { devices: [] }
              : pathname === '/sidebar' ? { sections: [], placements: [], cursor: 0 } : [];
      return route.fulfill({ json: body });
    });
    await page.goto('/');
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await page.getByRole('button', { name: 'Models', exact: true }).click();
    const model = page.getByRole('button', { name: /^Default model,/ });
    await expect(model).toContainText('Server default');
    await expect(model).toHaveAccessibleName('Default model, Server default');
    await model.click();
    const search = page.getByRole('textbox', { name: 'Search models' });
    await expect(search).toBeFocused();
    await expect(page.getByRole('radio', { name: 'Not set' })).toBeChecked();
    await expect(page.getByRole('radio', { name: 'Gpt 5.1 OpenAI' })).toHaveCount(1);
    await expect(page.getByRole('radio', { name: /Voice|Disabled|Unconnected/ })).toHaveCount(0);
    await search.fill('openai 5.1');
    await search.press('Enter');
    await expect(model).toContainText('Gpt 5.1 OpenAI');
    await expect(model).toHaveAccessibleName('Default model, Gpt 5.1 OpenAI');
    await expect.poll(() => writes.length).toBe(1);
    expect(writes[0]).toEqual({ defaultProvider: 'one', defaultModel: 'gpt-5-1' });
    const reasoning = page.getByRole('button', { name: /^Reasoning,/ });
    await expect(reasoning).toHaveAccessibleName('Reasoning, Off');
    await reasoning.click();
    await expect(page.getByRole('radio', { name: 'Off' })).toBeChecked();
    await expect(page.getByRole('radio', { name: 'Off' })).toBeFocused();
    await page.getByRole('radio', { name: 'High', exact: true }).click();
    await expect(page.getByRole('status')).toContainText('Couldn’t save model settings.');
    await expect(reasoning).toHaveAccessibleName('Reasoning, Off');
    await reasoning.click();
    await page.getByRole('radio', { name: 'Medium' }).click();
    await expect(reasoning).toHaveAccessibleName('Reasoning, Medium');
    await expect(page.getByRole('status')).toHaveCount(0);
    await expect.poll(() => writes.length).toBe(3);
    expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain('secret');
    offline = true;
    await page.getByRole('button', { name: 'Close settings' }).click();
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await page.getByRole('button', { name: 'Models', exact: true }).click();
    await expect(model).toContainText('Gpt 5.1 OpenAI');
    await expect(reasoning).toHaveAccessibleName('Reasoning, Medium');
    await reasoning.click();
    await expect(page.getByRole('radio', { name: 'Medium' })).toBeChecked();
    await page.getByRole('radio', { name: 'Medium' }).press('ArrowDown');
    await expect(page.getByRole('radio', { name: 'High', exact: true })).toBeFocused();
    await page.getByRole('radio', { name: 'High', exact: true }).press('Escape');
    await expect(reasoning).toBeFocused();
    await model.click();
    await expect(page.getByRole('radio', { name: 'Gpt 5.1 OpenAI', exact: true })).toBeChecked();
    await page.getByRole('textbox', { name: 'Search models' }).press('Escape');
    await expect(page.getByRole('textbox', { name: 'Search models' })).toHaveCount(0);
  });
}
