import { expect, test } from '@playwright/test';

test('profile instructions preserve newer typing across a slow save and panel close', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const thread = { id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] };
  let serverPrompt = 'Original';
  const writes: string[] = [];
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/agents/bot') {
      if (request.method() === 'PATCH') {
        const { prompt } = request.postDataJSON();
        writes.push(prompt);
        if (writes.length === 1) await held;
        serverPrompt = prompt;
      }
      return route.fulfill({ json: { agent, prompt: serverPrompt } });
    }
    const data = path === '/agents' ? [agent] : path === '/threads' ? [thread]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  await page.getByRole('tab', { name: 'Capabilities' }).click();
  const instructions = page.getByRole('textbox', { name: 'Instructions' });
  await expect(instructions).toHaveValue('Original');
  await instructions.fill('First edit');
  await expect.poll(() => writes.length).toBe(1);
  await instructions.fill('Newer edit');
  await page.getByRole('button', { name: 'Close side panel' }).click();
  release();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes).toEqual(['First edit', 'Newer edit']);
  await page.locator('main button.chat-header').click();
  await page.getByRole('tab', { name: 'Capabilities' }).click();
  await expect(instructions).toHaveValue('Newer edit');
});

test('a failed instructions save keeps the edit for reopening and retries', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const thread = { id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] };
  let prompt = 'Original';
  let attempts = 0;
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/agents/bot') {
      if (request.method() === 'PATCH') {
        attempts++;
        if (attempts === 1) return route.fulfill({ status: 503, json: { message: 'offline' } });
        prompt = request.postDataJSON().prompt;
      }
      return route.fulfill({ json: { agent, prompt } });
    }
    const data = path === '/agents' ? [agent] : path === '/threads' ? [thread]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  await page.getByRole('tab', { name: 'Capabilities' }).click();
  const instructions = page.getByRole('textbox', { name: 'Instructions' });
  await expect(instructions).toHaveValue('Original');
  await instructions.fill('Keep this edit');
  await expect(page.getByRole('status')).toContainText('Couldn’t save this change.');
  await page.getByRole('button', { name: 'Close side panel' }).click();
  await page.locator('main button.chat-header').click();
  await page.getByRole('tab', { name: 'Capabilities' }).click();
  await expect(instructions).toHaveValue('Keep this edit');
  await expect.poll(() => prompt).toBe('Keep this edit');
  await expect(page.getByRole('status')).toHaveCount(0);
});

test('refreshing a chat does not reset its open profile tab', async ({ page }) => {
  let sequence = 0;
  let reads = 0;
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
    if (path === '/threads') reads++;
    const data = path === '/agents/bot' ? { agent, prompt: 'Original' }
      : path === '/agents' ? [agent, { ...agent, id: 'other', name: 'Other' }]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: sequence, unreadCount: 0, runs: [] },
        { id: 'other', agentID: 'other', agentIDs: ['other'], title: 'Other', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  await page.getByRole('tab', { name: 'Capabilities' }).click();
  await expect(page.getByRole('textbox', { name: 'Instructions' })).toHaveValue('Original');
  sequence = 1;
  await expect.poll(() => reads, { timeout: 8000 }).toBeGreaterThan(1);
  await expect(page.getByRole('tab', { name: 'Capabilities' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: 'Instructions' })).toBeVisible();
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Other' }).click();
  await expect(page.getByRole('tab', { name: 'Work' })).toHaveAttribute('aria-selected', 'true');
});

test('profile fields merge remote changes and preserve edits during a slow save', async ({ page }) => {
  let agent = { id: 'bot', name: 'Bot', role: 'Helper', model: '', active: true };
  const thread = { id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] };
  let prompt = 'Original';
  const writes: { name: string; role: string; prompt: string }[] = [];
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/agents/bot') {
      if (request.method() === 'PATCH') {
        const update = request.postDataJSON();
        writes.push(update);
        if (writes.length === 1) await held;
        agent = { ...agent, name: update.name, role: update.role };
        prompt = update.prompt;
      }
      return route.fulfill({ json: { agent, prompt } });
    }
    const data = path === '/agents' ? [agent] : path === '/threads' ? [thread]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  const name = page.getByRole('textbox', { name: 'Name', exact: true });
  const role = page.getByRole('textbox', { name: 'Role', exact: true });
  await expect(role).toHaveValue('Helper');
  await name.fill('');
  await role.fill('Draft role');
  agent = { ...agent, role: 'Remote role' };
  prompt = 'Remote instructions';
  await page.waitForTimeout(5700);
  expect(writes).toHaveLength(0);
  await expect(name).toHaveValue('');
  await expect(role).toHaveValue('Draft role');
  await name.fill('  First name  ');
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).toEqual({ name: 'First name', role: 'Draft role', prompt: 'Remote instructions' });
  await name.fill('New name');
  await role.fill('New role');
  await page.getByRole('button', { name: 'Close side panel' }).click();
  release();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1]).toEqual({ name: 'New name', role: 'New role', prompt: 'Remote instructions' });
  await page.locator('main button.chat-header').click();
  await expect(name).toHaveValue('New name');
  await expect(role).toHaveValue('New role');
});

test('name and role drafts survive reload and retry after failure', async ({ page }) => {
  let agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const thread = { id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] };
  let fail = true;
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/agents/bot') {
      if (request.method() === 'PATCH') {
        if (fail) return route.fulfill({ status: 503, json: { message: 'offline' } });
        agent = { ...agent, ...request.postDataJSON() };
      }
      return route.fulfill({ json: { agent, prompt: 'Original' } });
    }
    const data = path === '/agents' ? [agent] : path === '/threads' ? [thread]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Saved draft');
  await page.getByRole('textbox', { name: 'Role', exact: true }).fill('Draft role');
  await expect(page.getByRole('status')).toContainText('Couldn’t save this change.');
  await page.reload();
  if (!await page.getByRole('textbox', { name: 'Name', exact: true }).isVisible()) {
    await page.locator('main button.chat-header').click();
  }
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue('Saved draft');
  await expect(page.getByRole('textbox', { name: 'Role', exact: true })).toHaveValue('Draft role');
  fail = false;
  await expect.poll(() => agent.name, { timeout: 8000 }).toBe('Saved draft');
  expect(agent.role).toBe('Draft role');
});
