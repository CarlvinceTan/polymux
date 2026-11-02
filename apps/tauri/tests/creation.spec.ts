import { expect, test } from '@playwright/test';

test('bot creation submits the selected avatar without creating a second chat', async ({ page }) => {
  const agents: Record<string, unknown>[] = [];
  const threads: Record<string, unknown>[] = [];
  const writes: { path: string; body: unknown }[] = [];
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (request.method() === 'POST') {
      writes.push({ path, body: request.postDataJSON() });
      if (path === '/agents') {
        const agent = { id: 'agent-1', ...request.postDataJSON(), active: true, model: '', provider: '' };
        agents.push(agent);
        threads.push({ id: agent.id, agentID: agent.id, agentIDs: [agent.id], title: agent.name, lastSeq: 0, unreadCount: 0 });
        return route.fulfill({ json: agent });
      }
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'New Bot', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'New Bot' })).toBeVisible();
  await page.getByRole('button', { name: 'Close New Bot' }).click();
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New Bot' }).click();
  await expect(page.getByRole('dialog', { name: 'New Bot' })).toBeVisible();
  expect(writes).toHaveLength(0);
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Aster');
  await page.getByRole('button', { name: 'Hexagon', exact: true }).click();
  await page.getByRole('button', { name: 'Grey', exact: true }).click();
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'New Bot' })).toHaveCount(0);
  const agent = writes.find(write => write.path === '/agents')?.body as { name: string; avatar: { shape: string; color: string } };
  expect(agent.name).toBe('Aster');
  expect(agent.avatar.shape).toBe('hexagone');
  expect(agent.avatar.color).toBe('gris');
  expect(writes.filter(write => write.path === '/threads')).toHaveLength(0);
});

test('new bot and group drafts survive dismissal until they are created', async ({ page }) => {
  const agents = ['Alpha', 'Beta'].map((name, i) => ({ id: `agent-${i}`, name, role: '', active: true, model: '', provider: '' }));
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    const body = path === '/agents' ? agents : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'New Bot', exact: true }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Draft bot');
  await page.getByRole('button', { name: 'Heart', exact: true }).click();
  await page.getByRole('button', { name: 'Close New Bot' }).click();
  await page.getByRole('button', { name: 'New Bot', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue('Draft bot');
  await expect(page.getByRole('button', { name: 'Heart', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await page.getByRole('button', { name: 'Close New Bot' }).click();

  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New Group Chat' }).click();
  await page.getByRole('button', { name: 'Select Alpha' }).click();
  await page.getByRole('button', { name: 'Select Beta' }).click();
  await page.getByRole('textbox', { name: 'Chat name (optional)' }).fill('Draft group');
  await page.getByRole('button', { name: 'Close New Group Chat' }).click();
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New Group Chat' }).click();
  await expect(page.getByRole('button', { name: 'Remove Alpha' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Remove Beta' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Chat name (optional)' })).toHaveValue('Draft group');
});

test('a group requires two selected bots and sends the chosen members and name', async ({ page }) => {
  const agents = ['Alpha', 'Beta'].map((name, i) => ({ id: `agent-${i}`, name, role: '', active: true, model: '', provider: '' }));
  let created: unknown;
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/threads' && request.method() === 'POST') {
      created = request.postDataJSON();
      return route.fulfill({ json: { id: 'group', agentID: 'agent-0', agentIDs: ['agent-0','agent-1'], title: 'Team', unreadCount: 0, lastSeq: 0 } });
    }
    const body = path === '/agents' ? agents : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New Group Chat' }).click();
  const create = page.getByRole('button', { name: 'Create', exact: true });
  await expect(create).toBeDisabled();
  await page.getByRole('button', { name: 'Select Alpha' }).click();
  await expect(create).toBeDisabled();
  await page.getByRole('button', { name: 'Select Beta' }).click();
  await page.getByRole('textbox', { name: 'Chat name (optional)' }).fill('Team');
  await create.click();
  await expect(page.getByRole('dialog', { name: 'New Group Chat' })).toHaveCount(0);
  expect(created).toEqual({ agentIDs: ['agent-0', 'agent-1'], title: 'Team' });
});
