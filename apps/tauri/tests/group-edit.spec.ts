import { expect, test } from '@playwright/test';

test('group edits preserve newer members and name across a slow save and panel close', async ({ page }) => {
  const agents = ['one', 'two', 'three'].map((id) => ({ id, name: id, role: '', model: '', active: true }));
  let thread = { id: 'group', agentID: 'one', agentIDs: ['one', 'two'], title: 'Original', lastSeq: 0, unreadCount: 0, runs: [] };
  const writes: { title: string; agentIDs: string[] }[] = [];
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/group' && request.method() === 'PATCH') {
      const edit = request.postDataJSON();
      writes.push(edit);
      if (writes.length === 1) await held;
      thread = { ...thread, ...edit };
      return route.fulfill({ json: thread });
    }
    const data = path === '/agents' ? agents : path === '/threads' ? [thread]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  const name = page.getByRole('textbox', { name: 'Chat name' });
  await expect(name).toHaveValue('Original');
  await name.fill('New name');
  await expect.poll(() => writes.length).toBe(1);
  await page.waitForTimeout(3400);
  await expect(page.locator('main button.chat-header')).toContainText('New name');
  await page.getByRole('button', { name: 'Add agents to chat' }).click();
  await page.getByRole('textbox', { name: 'Search agents to add' }).fill('three');
  await page.getByRole('button', { name: 'Add three to chat' }).click();
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  await expect(page.getByText('three', { exact: true }).last()).toBeVisible();
  await page.getByRole('button', { name: 'Close side panel' }).click();
  release();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes.map(({ title, agentIDs }) => ({ title, agentIDs }))).toEqual([
    { title: 'New name', agentIDs: ['one', 'two'] },
    { title: 'New name', agentIDs: ['one', 'two', 'three'] },
  ]);
  await page.locator('main button.chat-header').click();
  await expect(name).toHaveValue('New name');
  await page.getByRole('button', { name: 'Edit one’s profile' }).hover();
  await page.getByRole('button', { name: 'Remove one from chat' }).click();
  await expect.poll(() => thread.agentIDs).toEqual(['two', 'three']);
  await page.getByRole('button', { name: 'Edit two’s profile' }).hover();
  await page.getByRole('button', { name: 'Remove two from chat' }).click();
  await expect.poll(() => thread.agentIDs).toEqual(['three']);
  await page.getByRole('button', { name: 'Edit three’s profile' }).hover();
  await expect(page.getByRole('button', { name: 'Remove three from chat' })).toBeDisabled();
});

test('group changes survive a failed save and reload', async ({ page }) => {
  const agents = ['one', 'two'].map((id) => ({ id, name: id, role: '', model: '', active: true }));
  let thread = { id: 'group', agentID: 'one', agentIDs: ['one', 'two'], title: 'Original', lastSeq: 0, unreadCount: 0, runs: [] };
  let fail = true;
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/threads/group' && request.method() === 'PATCH') {
      if (fail) return route.fulfill({ status: 503, json: { message: 'offline' } });
      thread = { ...thread, ...request.postDataJSON() };
      return route.fulfill({ json: thread });
    }
    const data = path === '/agents' ? agents : path === '/threads' ? [thread]
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('main button.chat-header').click();
  await page.getByRole('textbox', { name: 'Chat name' }).fill('Pending name');
  await expect(page.getByRole('status')).toHaveText('Changes haven’t synced yet.');
  await page.getByRole('button', { name: 'Close side panel' }).click();
  await page.reload();
  await expect(page.locator('main button.chat-header')).toContainText('Pending name');
  fail = false;
  await expect.poll(() => thread.title, { timeout: 8000 }).toBe('Pending name');
  await page.locator('main button.chat-header').click();
  await expect(page.getByRole('textbox', { name: 'Chat name' })).toHaveValue('Pending name');
  await page.getByRole('button', { name: 'Edit one’s profile' }).click();
  await expect(page.getByRole('tab', { name: 'Capabilities' })).toBeVisible();
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Chat name' })).toHaveValue('Pending name');
});

test('group retries stop on session disposal and resume in the next session', async ({ page }) => {
  await page.goto('/');
  const result = await page.evaluate(async () => {
    const { Session } = await import('/lib/session.svelte.ts');
    const { groupEdit, restoreGroups } = await import('/lib/shell/groupEdit.ts');
    const thread = { id: 'group', agentID: 'one', agentIDs: ['one', 'two'], title: 'Original', lastReadSeq: 0, lastSeq: 0, unreadCount: 0, runs: [] };
    let attempts = 0;
    let fail = true;
    const client = { updateThread: async (_id: string, title: string, agentIDs: string[]) => {
      attempts++;
      if (fail) throw new Error('offline');
      return { ...thread, title, agentIDs };
    } };
    const first = new Session('http://test-group', client as never);
    first.threads = [thread];
    groupEdit(first, thread).change({ title: 'Pending', agentIDs: ['one', 'two'] }, 0);
    await new Promise((resolve) => setTimeout(resolve, 30));
    first.dispose();
    await new Promise((resolve) => setTimeout(resolve, 5200));
    const afterDispose = attempts;
    fail = false;
    const second = new Session('http://test-group', client as never);
    second.threads = [thread];
    restoreGroups(second);
    await new Promise((resolve) => setTimeout(resolve, 30));
    const title = second.threads[0].title;
    second.dispose();
    return { afterDispose, attempts, title };
  });
  expect(result).toEqual({ afterDispose: 1, attempts: 2, title: 'Pending' });
});
