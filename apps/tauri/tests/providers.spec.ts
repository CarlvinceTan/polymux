import { expect, test } from '@playwright/test';

for (const colorScheme of ['light', 'dark'] as const) {
  test(`provider sign-in and management keep credentials transient (${colorScheme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme });
    const options = [{ type: 'openai', name: 'OpenAI', auth: 'api_key', label: 'API key', subscription: false }];
    let providers: any[] = [];
    let login: any = { id: 'login', revision: 1, events: [], prompt: { id: 1, type: 'secret', message: 'Key' }, done: false };
    let unblock: (() => void) | undefined;
    let failToggle = true;
    const writes: { path: string; body: any }[] = [];
    await page.route('http://127.0.0.1:8787/**', async (route) => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      const method = request.method();
      if (path === '/providers/options') return route.fulfill({ json: options });
      if (path === '/providers' && method === 'GET') return route.fulfill({ json: providers });
      if (path === '/providers/logins' && method === 'POST') return route.fulfill({ json: login });
      if (path === '/providers/logins/login' && method === 'GET') {
        if (!login.done) await new Promise<void>((resolve) => { unblock = resolve; });
        return route.fulfill({ json: login });
      }
      if (path === '/providers/logins/login/answer') {
        writes.push({ path, body: request.postDataJSON() });
        providers = [{ id: 'openai-one', type: 'openai', name: 'API key 1', baseURL: 'https://example.test', enabled: true, order: 0, status: 'api_key', keyHint: 'priv••••-key', models: [{ id: 'gpt-5-1', tags: ['text'], enabled: true }] }];
        login = { id: 'login', revision: 2, events: [], done: true, provider: providers[0] };
        unblock?.();
        return route.fulfill({ status: 204 });
      }
      if (path === '/providers/groups/openai') {
        const body = request.postDataJSON(); writes.push({ path, body });
        if (failToggle) { failToggle = false; return route.fulfill({ status: 500, json: {} }); }
        providers = providers.map((provider) => ({ ...provider, enabled: body.enabled ?? provider.enabled }));
        return route.fulfill({ json: providers });
      }
      if (path === '/providers/openai-one' && method === 'PATCH') {
        const body = request.postDataJSON(); writes.push({ path, body });
        providers = providers.map((provider) => ({ ...provider, ...body }));
        return route.fulfill({ json: providers[0] });
      }
      if (path === '/providers/openai-one' && method === 'DELETE') {
        providers = [];
        return route.fulfill({ status: 204 });
      }
      const body = path === '/settings' ? { appearance: 'system', defaultProvider: '', defaultModel: '', defaultReasoning: 'off' }
        : path === '/network' ? { devices: [] } : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 } : [];
      return route.fulfill({ json: body });
    });
    await page.goto('/');
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await page.getByRole('button', { name: 'Models', exact: true }).click();
    await expect(page.getByText('No providers yet.')).toBeVisible();
    await page.getByRole('button', { name: 'Add provider' }).click();
    await page.getByRole('textbox', { name: 'Search providers' }).fill('openai');
    await page.getByRole('button', { name: 'OpenAI · API key' }).click();
    await page.getByLabel('API key').fill('private-login-key');
    await page.getByRole('button', { name: 'Continue' }).click();
    await expect(page.getByRole('button', { name: 'Edit OpenAI' })).toBeVisible();
    expect(writes[0].body).toEqual({ prompt: 1, value: 'private-login-key' });
    expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain('private-login-key');
    await page.getByRole('button', { name: 'Default model, Server default' }).click();
    await expect(page.getByRole('radio', { name: 'Gpt 5.1 OpenAI' })).toBeVisible();
    await page.getByRole('button', { name: 'Default model, Server default' }).click();
    await page.getByRole('button', { name: 'Edit OpenAI' }).click();
    await expect(page.getByText('priv••••-key')).toBeVisible();
    const enabled = page.getByRole('checkbox', { name: 'Enabled' });
    await enabled.click();
    await expect(page.getByRole('status')).toContainText('Couldn’t save provider settings.');
    await expect(enabled).toBeChecked();
    await enabled.uncheck();
    await expect(enabled).not.toBeChecked();
    await page.getByRole('button', { name: 'Edit API key 1' }).click();
    await page.getByLabel('Name', { exact: true }).fill('Work');
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Remove Work' })).toBeVisible();
    await page.getByRole('button', { name: 'Remove Work' }).click();
    await page.getByRole('alertdialog').getByRole('button', { name: 'Remove', exact: true }).click();
    await expect(page.getByText('No providers yet.')).toBeVisible();
  });
}

test('provider sign-in handles multi-step prompts and cancels a pending login', async ({ page }) => {
  const option = { type: 'fixture', name: 'Subscription', auth: 'oauth', label: 'Subscription sign-in', subscription: true };
  let providers: any[] = [];
  let login: any;
  let wake: (() => void) | undefined;
  let cancelled = false;
  const answers: any[] = [];
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const method = request.method();
    if (path === '/providers/options') return route.fulfill({ json: [option] });
    if (path === '/providers' && method === 'GET') return route.fulfill({ json: providers });
    if (path === '/providers/logins' && method === 'POST') {
      login = { id: 'oauth', revision: 1, events: [{ type: 'device_code', userCode: 'TEST-1234', verificationUri: 'https://example.invalid/device' }], prompt: { id: 1, type: 'manual_code', message: 'Paste the redirect URL', placeholder: 'http://localhost/callback' }, done: false };
      return route.fulfill({ json: login });
    }
    if (path === '/providers/logins/oauth' && method === 'DELETE') {
      cancelled = true; login = { ...login, done: true }; wake?.();
      return route.fulfill({ status: 204 });
    }
    if (path === '/providers/logins/oauth' && method === 'GET') {
      if (Number(new URL(request.url()).searchParams.get('after')) === login.revision && !login.done) await new Promise<void>((resolve) => { wake = resolve; });
      return route.fulfill({ json: login });
    }
    if (path === '/providers/logins/oauth/answer') {
      const body = request.postDataJSON(); answers.push(body);
      if (body.prompt === 1) login = { ...login, revision: 2, prompt: { id: 2, type: 'select', message: 'Choose an account', options: [{ id: 'personal', label: 'Personal' }] } };
      else {
        providers = [{ id: 'account', type: 'fixture', name: 'Personal', baseURL: 'https://example.invalid', enabled: true, order: 0, status: 'oauth', models: [{ id: 'subscription-chat', tags: ['text'], enabled: true }] }];
        login = { ...login, revision: 3, prompt: undefined, done: true, provider: providers[0] };
      }
      wake?.();
      return route.fulfill({ status: 204 });
    }
    const body = path === '/settings' ? { appearance: 'system', defaultProvider: '', defaultModel: '', defaultReasoning: 'off' }
      : path === '/network' ? { devices: [] } : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Models', exact: true }).click();
  await page.getByRole('button', { name: 'Add provider' }).click();
  await page.getByRole('button', { name: 'Subscription · Subscription sign-in' }).click();
  await expect(page.getByText('TEST-1234')).toBeVisible();
  await page.getByRole('textbox', { name: 'http://localhost/callback' }).fill('http://localhost/callback?code=test');
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('button', { name: 'Personal', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Edit Subscription' })).toBeVisible();
  expect(answers).toEqual([{ prompt: 1, value: 'http://localhost/callback?code=test' }, { prompt: 2, value: 'personal' }]);
  await page.getByRole('button', { name: 'Add provider' }).click();
  await page.getByRole('button', { name: 'Subscription · Subscription sign-in' }).click();
  await expect(page.getByText('TEST-1234')).toBeVisible();
  await page.getByRole('button', { name: 'Back' }).click();
  await expect.poll(() => cancelled).toBe(true);
  await expect(page.getByRole('textbox', { name: 'Search providers' })).toBeVisible();
});

test('custom provider with an explicit model can be added and removed without discovery', async ({ page }) => {
  let providers: any[] = [];
  let settings = { appearance: 'system', defaultProvider: '', defaultModel: '', defaultReasoning: 'off' };
  const paths: string[] = [];
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const method = request.method();
    paths.push(`${method} ${path}`);
    if (path === '/providers/options') return route.fulfill({ json: [] });
    if (path === '/providers' && method === 'GET') return route.fulfill({ json: providers });
    if (path === '/providers' && method === 'POST') { providers = [request.postDataJSON()]; return route.fulfill({ json: providers[0] }); }
    if (path.startsWith('/providers/custom-') && method === 'DELETE') { providers = []; settings = { ...settings, defaultProvider: '', defaultModel: '' }; return route.fulfill({ status: 204 }); }
    if (path === '/settings' && method === 'PATCH') { settings = { ...settings, ...request.postDataJSON() }; return route.fulfill({ json: settings }); }
    const body = path === '/settings' ? settings : path === '/network' ? { devices: [] }
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Models', exact: true }).click();
  await page.getByRole('button', { name: 'Add provider' }).click();
  await page.getByRole('button', { name: 'Add custom provider' }).click();
  await page.getByLabel('Base URL').fill('http://127.0.0.1:1/v1');
  await page.getByLabel('Model ID').fill('fixture-model');
  await page.getByRole('button', { name: 'Add', exact: true }).click();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Edit Custom' })).toBeVisible();
  expect(paths.some((path) => path.endsWith('/test'))).toBe(false);
  await page.getByRole('button', { name: 'Default model, Server default' }).click();
  await page.getByRole('radio', { name: 'Fixture Model Custom' }).click();
  await expect(page.getByRole('button', { name: 'Default model, Fixture Model Custom' })).toBeVisible();
  await page.getByRole('button', { name: 'Edit Custom' }).click();
  await page.getByRole('button', { name: 'Delete' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Remove' }).click();
  await expect(page.getByText('No providers yet.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Default model, Server default' })).toBeVisible();
});

test('a stale provider refresh cannot overwrite a saved group change', async ({ page }) => {
  const provider = { id: 'cached', type: 'openai', name: 'Work', baseURL: '', enabled: true, order: 0, status: 'api_key', models: [{ id: 'gpt-test', tags: ['text'], enabled: true }] };
  const option = { type: 'openai', name: 'OpenAI', auth: 'api_key', label: 'API key', subscription: false };
  await page.addInitScript(({ provider, option }) => localStorage.setItem('polymux.models.http://127.0.0.1:8787', JSON.stringify({ providers: [provider], options: [option] })), { provider, option });
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  let requested = false;
  let serverProvider = provider;
  let offline = false;
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (offline && (path === '/providers' || path === '/providers/options')) return route.fulfill({ status: 503 });
    if (path === '/providers/options') return route.fulfill({ json: [option] });
    if (path === '/providers' && request.method() === 'GET') { const snapshot = serverProvider; requested = true; await held; return route.fulfill({ json: [snapshot] }); }
    if (path === '/providers/groups/openai') { serverProvider = { ...provider, enabled: false }; return route.fulfill({ json: [serverProvider] }); }
    const body = path === '/settings' ? { appearance: 'system', defaultProvider: '', defaultModel: '', defaultReasoning: 'off' }
      : path === '/network' ? { devices: [] } : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Models', exact: true }).click();
  await expect.poll(() => requested).toBe(true);
  await page.getByRole('button', { name: 'Edit OpenAI' }).click();
  const enabled = page.getByRole('checkbox', { name: 'Enabled' });
  await enabled.uncheck();
  await expect(enabled).not.toBeChecked();
  release();
  await expect(enabled).not.toBeChecked();
  await page.getByRole('button', { name: 'Back' }).click();
  await expect(page.getByText('1 model (0 active)')).toBeVisible();
  offline = true;
  await page.getByRole('button', { name: 'Close settings' }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Models', exact: true }).click();
  await expect(page.getByText('1 model (0 active)')).toBeVisible();
});
