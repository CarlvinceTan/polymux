import { expect, test } from '@playwright/test';

const original = { shape: 'square', color: 'rouge', expression: 'heureux', followCursor: false, animates: false, playSequence: true, blocks: [{ id: 'one', state: 'idle', duration: 2 }] };
const chat = (id: string, name: string) => ({ id, agentID: id, agentIDs: [id], title: name, lastSeq: 0, unreadCount: 0, runs: [] });

for (const mode of ['light', 'dark'] as const) test(`profile avatar edits preserve the full design and flush newer changes on close (${mode})`, async ({ page }) => {
  await page.emulateMedia({ colorScheme: mode });
  let agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true, avatar: original };
  const writes: typeof original[] = [];
  let agentReads = 0;
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/agents') agentReads++;
    if (path === '/agents/bot/avatar') {
      const avatar = request.postDataJSON() as typeof original;
      writes.push(avatar);
      if (writes.length === 1) await held;
      agent = { ...agent, avatar };
      return route.fulfill({ json: agent });
    }
    const data = path === '/agents/bot' ? { agent, prompt: '' }
      : path === '/agents' ? [agent] : path === '/threads' ? [chat('bot', 'Bot')]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  await page.getByRole('button', { name: 'Edit avatar' }).click();
  const editor = page.getByRole('dialog', { name: 'Bot’s avatar' });
  await expect(editor.getByRole('button', { name: 'Square', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(editor.getByRole('button', { name: 'Red', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await editor.getByRole('button', { name: 'Cloud', exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  await editor.getByRole('button', { name: 'Animals', exact: true }).click();
  await editor.getByRole('button', { name: 'Cat', exact: true }).click();
  await expect(editor.getByRole('button', { name: 'Cat', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await editor.getByRole('button', { name: 'Close avatar editor' }).click();
  const portrait = page.getByRole('button', { name: 'Edit avatar' }).locator('canvas');
  const optimistic = await portrait.evaluate((canvas: HTMLCanvasElement) => canvas.toDataURL());
  await expect.poll(() => agentReads, { timeout: 8000 }).toBeGreaterThan(1);
  await expect.poll(() => portrait.evaluate((canvas: HTMLCanvasElement) => canvas.toDataURL())).toBe(optimistic);
  release();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes).toEqual([{ ...original, shape: 'nuage' }, { ...original, shape: 'cat' }]);
  await expect.poll(() => page.evaluate(() => Object.keys(localStorage).filter((key) => key.startsWith('polymux.avatar.')).length)).toBe(0);
  await page.getByRole('button', { name: 'Edit avatar' }).click();
  await expect(editor.getByRole('button', { name: 'Animals', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(editor.getByRole('button', { name: 'Cat', exact: true })).toHaveAttribute('aria-pressed', 'true');
});

test('a chat menu edits the intended bot and retries a failed save after closing', async ({ page }) => {
  const agents = [
    { id: 'bot', name: 'Bot', role: '', model: '', active: true, avatar: original },
    { id: 'other', name: 'Other', role: '', model: '', active: true, avatar: original },
  ];
  let attempts = 0;
  let saved = original;
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/agents/other/avatar') {
      attempts++;
      if (attempts === 1) return route.fulfill({ status: 503, json: { message: 'offline' } });
      saved = request.postDataJSON();
      return route.fulfill({ json: { ...agents[1], avatar: saved } });
    }
    const data = path === '/agents' ? agents : path === '/threads' ? [chat('bot', 'Bot'), chat('other', 'Other')]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Other' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Customise avatar' }).click();
  const editor = page.getByRole('dialog', { name: 'Other’s avatar' });
  await editor.getByRole('button', { name: 'Animals', exact: true }).click();
  await editor.getByRole('button', { name: 'Dog', exact: true }).click();
  await expect(editor.getByRole('status')).toHaveText('Couldn’t save the avatar.');
  await editor.getByRole('button', { name: 'Close avatar editor' }).click();
  await expect.poll(() => saved.shape).toBe('dog');
  expect(saved).toEqual({ ...original, shape: 'dog' });
  expect(attempts).toBeGreaterThanOrEqual(2);
});

test('a pending avatar survives reload and saves when the server returns', async ({ page }) => {
  let agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true, avatar: original };
  let online = false;
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/agents/bot/avatar') {
      if (!online) return route.fulfill({ status: 503, json: { message: 'offline' } });
      agent = { ...agent, avatar: request.postDataJSON() };
      return route.fulfill({ json: agent });
    }
    const data = path === '/agents/bot' ? { agent, prompt: '' }
      : path === '/agents' ? [agent] : path === '/threads' ? [chat('bot', 'Bot')]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } }
      : path.endsWith('/configuration') ? { skills: [], tools: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  await page.getByRole('button', { name: 'Edit avatar' }).click();
  const editor = page.getByRole('dialog', { name: 'Bot’s avatar' });
  await editor.getByRole('button', { name: 'Animals', exact: true }).click();
  await editor.getByRole('button', { name: 'Fox', exact: true }).click();
  await expect(editor.getByRole('status')).toHaveText('Couldn’t save the avatar.');
  online = true;
  await page.reload();
  await expect.poll(() => agent.avatar.shape, { timeout: 8000 }).toBe('fox');
  expect(agent.avatar).toEqual({ ...original, shape: 'fox' });
});
