import { expect, test } from '@playwright/test';

test('section names validate, rename, and deletion moves chats to Unassigned', async ({ page }) => {
  let sections = [{ id: '__unassigned__', name: 'Unassigned', collapsed: false, order: 0 }, { id: 'team', name: 'Team', collapsed: false, order: 1 }];
  const agents = [{ id: 'bot', name: 'Alpha', role: '', active: true, model: '', provider: '' }];
  let threads = [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Alpha', lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID: 'team' as string | undefined }];
  const writes: { method: string; path: string; body: unknown }[] = [];
  await page.route('http://127.0.0.1:8787/**', async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/sections/team' && request.method() === 'PATCH') {
      const body = request.postDataJSON();
      writes.push({ method: 'PATCH', path, body });
      sections = sections.map(section => section.id === 'team' ? { ...section, ...body } : section);
      return route.fulfill({ json: sections.find(section => section.id === 'team') });
    }
    if (path === '/sections/team' && request.method() === 'DELETE') {
      writes.push({ method: 'DELETE', path, body: null });
      sections = sections.filter(section => section.id !== 'team');
      threads = threads.map(thread => ({ ...thread, sectionID: undefined }));
      return route.fulfill({ status: 204 });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Collapse Team' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Rename' }).click();
  const editor = page.getByRole('textbox', { name: 'Section name' });
  await editor.fill('x'.repeat(41));
  await editor.press('Enter');
  await expect(page.getByRole('status')).toHaveText('Section names are limited to 40 characters.');
  expect(writes).toHaveLength(0);
  await editor.fill('Work');
  await editor.press('Enter');
  await expect(page.getByRole('button', { name: 'Collapse Work' })).toBeVisible();
  await page.getByRole('button', { name: 'Collapse Work' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete section' }).click();
  const dialog = page.getByRole('alertdialog', { name: 'Delete Work?' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Delete section', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Collapse Unassigned' })).toBeVisible();
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Alpha' })).toBeVisible();
  expect(writes).toEqual([{ method: 'PATCH', path: '/sections/team', body: { name: 'Work' } }, { method: 'DELETE', path: '/sections/team', body: null }]);
});

test('a failed section delete restores its chats and shows one status', async ({ page }) => {
  const sections = [{ id: 'team', name: 'Team', collapsed: false, order: 1 }];
  const agents = [{ id: 'bot', name: 'Alpha', role: '', active: true, model: '', provider: '' }];
  const threads = [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Alpha', lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID: 'team' }];
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/sections/team' && request.method() === 'DELETE') return route.fulfill({ status: 503, json: { message: 'offline' } });
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Collapse Team' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete section' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete section', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Couldn’t delete this section.');
  await expect(page.getByRole('button', { name: 'Collapse Team' })).toBeVisible();
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Alpha' })).toBeVisible();
});

test('create section offers an editable unique name and sends only the confirmed name', async ({ page }) => {
  const sections = [{ id: 'existing', name: 'New section', collapsed: false, order: 0 }];
  let created = '';
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/sections' && request.method() === 'POST') {
      created = request.postDataJSON().name;
      sections.push({ id: 'new', name: created, collapsed: false, order: 1 });
      return route.fulfill({ json: { id: 'new', name: created, collapsed: false, order: 1 } });
    }
    const body = path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.locator('.list').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Create section' }).click();
  const editor = page.getByRole('textbox', { name: 'Section name' });
  await expect(editor).toHaveValue('New section 2');
  await editor.fill('Research');
  await editor.press('Enter');
  await expect(page.getByRole('button', { name: 'Collapse Research' })).toBeVisible();
  expect(created).toBe('Research');
});

test('a chat menu can create a section and move its chat into it', async ({ page }) => {
  let sections = [{ id: '__unassigned__', name: 'Unassigned', collapsed: false, order: 0 }];
  let threads = [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Alpha', lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID: undefined as string | undefined }];
  const agents = [{ id: 'bot', name: 'Alpha', role: '', active: true, model: '', provider: '' }];
  const writes: unknown[] = [];
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/sections' && request.method() === 'POST') {
      const name = request.postDataJSON().name;
      const created = { id: 'research', name, collapsed: false, order: 1 };
      sections = [...sections, created];
      return route.fulfill({ json: created });
    }
    if (path === '/threads/bot/placement' && request.method() === 'PATCH') {
      const body = request.postDataJSON();
      writes.push(body);
      threads = threads.map(thread => thread.id === 'bot' ? { ...thread, ...body, sectionOrder: 0 } : thread);
      return route.fulfill({ json: threads[0] });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Alpha' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Create section' }).click();
  const editor = page.getByRole('textbox', { name: 'Section name' });
  await expect(editor).toHaveValue('New section');
  await editor.fill('Research');
  await editor.press('Enter');
  await expect(page.getByRole('button', { name: 'Collapse Research' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Collapse Research' }).locator('..')).toContainText('Alpha');
  await expect.poll(() => writes).toEqual([{ pinned: false, sectionID: 'research' }]);
});

test('a pending rename survives refresh and failed rename preserves an unrelated new bot', async ({ page }) => {
  const sections = [{ id: 'team', name: 'Team', collapsed: false, order: 0 }];
  const agents = [{ id: 'bot', name: 'Alpha', role: '', active: true, model: '', provider: '' }];
  const threads = [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Alpha', lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID: 'team' }];
  let release!: () => void;
  let requested!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  const started = new Promise<void>(resolve => { requested = resolve; });
  let refreshes = 0;
  await page.route('http://127.0.0.1:8787/**', async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/sections/team' && request.method() === 'PATCH') {
      requested();
      await pending;
      return route.fulfill({ status: 503, json: { message: 'offline' } });
    }
    if (path === '/agents' && request.method() === 'POST') {
      const agent = { id: 'new-bot', ...request.postDataJSON(), role: '', active: true, model: '', provider: '' };
      agents.push(agent);
      threads.push({ id: agent.id, agentID: agent.id, agentIDs: [agent.id], title: agent.name, lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID: '' });
      return route.fulfill({ json: agent });
    }
    if (path === '/sidebar') refreshes++;
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Collapse Team' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Rename' }).click();
  await page.getByRole('textbox', { name: 'Section name' }).fill('Work');
  await page.getByRole('textbox', { name: 'Section name' }).press('Enter');
  await started;
  await expect(page.getByRole('button', { name: 'Collapse Work' })).toBeVisible();
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New Bot' }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Beta');
  await page.getByRole('dialog', { name: 'New Bot' }).getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Beta' })).toBeVisible();
  await expect.poll(() => refreshes, { timeout: 7000 }).toBeGreaterThan(2);
  await expect(page.getByRole('button', { name: 'Collapse Work' })).toBeVisible();
  release();
  await expect(page.getByRole('status')).toHaveText('Couldn’t rename this section.');
  await expect(page.getByRole('button', { name: 'Collapse Team' })).toBeVisible();
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Beta' })).toBeVisible();
});

test('failed section deletion restores its own chat without discarding a bot created meanwhile', async ({ page }) => {
  const sections = [{ id: 'team', name: 'Team', collapsed: false, order: 0 }];
  const agents = [{ id: 'bot', name: 'Alpha', role: '', active: true, model: '', provider: '' }];
  const threads = [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Alpha', lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID: 'team' }];
  let release!: () => void;
  let requested!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  const started = new Promise<void>(resolve => { requested = resolve; });
  await page.route('http://127.0.0.1:8787/**', async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/sections/team' && request.method() === 'DELETE') {
      requested(); await pending;
      return route.fulfill({ status: 503, json: { message: 'offline' } });
    }
    if (path === '/agents' && request.method() === 'POST') {
      const agent = { id: 'new-bot', ...request.postDataJSON(), role: '', active: true, model: '', provider: '' };
      agents.push(agent);
      threads.push({ id: agent.id, agentID: agent.id, agentIDs: [agent.id], title: agent.name, lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID: '' });
      return route.fulfill({ json: agent });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Collapse Team' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete section' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete section and chats' }).click();
  await started;
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New Bot' }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Beta');
  await page.getByRole('dialog', { name: 'New Bot' }).getByRole('button', { name: 'Create', exact: true }).click();
  const sidebar = page.getByRole('complementary', { name: 'Sidebar' });
  await expect(sidebar.getByRole('button', { name: 'Beta' })).toBeVisible();
  release();
  await expect(page.getByRole('status')).toHaveText('Couldn’t delete this section.');
  await expect(page.getByRole('button', { name: 'Collapse Team' })).toBeVisible();
  await expect(sidebar.getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect(sidebar.getByRole('button', { name: 'Beta' })).toBeVisible();
});

test('section editor survives transient focus loss after its menu closes', async ({ page }) => {
  const sections = [{ id: 'team', name: 'Team', collapsed: false, order: 0 }];
  await page.route('http://127.0.0.1:8787/**', route => {
    const path = new URL(route.request().url()).pathname;
    const body = path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Collapse Team' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Create section' }).click();
  const editor = page.getByRole('textbox', { name: 'Section name' });
  await expect(editor).toBeFocused();
  await editor.evaluate(element => (element as HTMLInputElement).blur());
  await expect(editor).toBeFocused();
  await editor.fill('Research');
  await page.waitForTimeout(650);
  await editor.evaluate(element => (element as HTMLInputElement).blur());
  await expect(editor).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Collapse Research' })).toHaveCount(0);
});

test('rename ignores the menu blur but saves when the user leaves the field', async ({ page }) => {
  let writes = 0;
  const sections = [{ id: 'team', name: 'Team', collapsed: false, order: 0 }];
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/sections/team' && request.method() === 'PATCH') {
      writes++;
      sections[0].name = request.postDataJSON().name;
      return route.fulfill({ json: sections[0] });
    }
    const body = path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Collapse Team' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Rename' }).click();
  const editor = page.getByRole('textbox', { name: 'Section name' });
  await expect(editor).toBeFocused();
  await editor.evaluate(element => (element as HTMLInputElement).blur());
  await expect(editor).toBeFocused();
  expect(writes).toBe(0);
  await editor.fill('Work');
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Collapse Work' })).toBeVisible();
  expect(writes).toBe(1);
});

test('section menu moves sections with disabled edge actions and rolls back a failed reorder', async ({ page }) => {
  let sections = [
    { id: 'one', name: 'One', collapsed: false, order: 0 },
    { id: 'two', name: 'Two', collapsed: false, order: 1 },
    { id: 'three', name: 'Three', collapsed: false, order: 2 },
  ];
  const orders: unknown[] = [];
  let fail = false;
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/sidebar/reorder' && request.method() === 'POST') {
      const body = request.postDataJSON(); orders.push(body);
      if (fail) return route.fulfill({ status: 503, json: { message: 'offline' } });
      const source = sections.findIndex(section => section.id === body.id);
      const [section] = sections.splice(source, 1);
      sections.splice(sections.findIndex(item => item.id === body.beforeID), 0, section);
      sections = sections.map((item, order) => ({ ...item, order }));
      return route.fulfill({ json: { id: body.id, order: 0 } });
    }
    const body = path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Collapse One' }).click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Move up' })).toBeDisabled();
  await expect(page.getByRole('menuitem', { name: 'Move down' })).toBeEnabled();
  await page.getByRole('menuitem', { name: 'Move down' }).click();
  await expect.poll(async () => (await page.locator('.section .head').allTextContents()).map(text => text.trim())).toEqual(['Two', 'One', 'Three']);
  expect(orders[0]).toEqual({ kind: 'sections', id: 'two', beforeID: 'one' });
  await page.getByRole('button', { name: 'Collapse Three' }).click({ button: 'right' });
  await expect(page.getByRole('menuitem', { name: 'Move down' })).toBeDisabled();
  fail = true;
  await page.getByRole('menuitem', { name: 'Move up' }).click();
  await expect(page.getByRole('status')).toHaveText('Couldn’t move this section.');
  await expect.poll(async () => (await page.locator('.section .head').allTextContents()).map(text => text.trim())).toEqual(['Two', 'One', 'Three']);
});

test('new bot created from a section retains that placement', async ({ page }) => {
  const sections = [{ id: 'team', name: 'Team', collapsed: false, order: 0 }];
  const agents: Record<string, unknown>[] = [];
  const threads: Record<string, unknown>[] = [];
  let created: Record<string, unknown> | undefined;
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/agents' && request.method() === 'POST') {
      created = request.postDataJSON();
      const agent = { id: 'bot', ...created, active: true, model: '', provider: '' };
      agents.push(agent);
      threads.push({ id: agent.id, agentID: agent.id, agentIDs: [agent.id], title: agent.name, lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID: 'team' });
      return route.fulfill({ json: agent });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Collapse Team' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'New Bot' }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Beta');
  await page.getByRole('dialog', { name: 'New Bot' }).getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Beta' })).toBeVisible();
  expect(created?.sectionID).toBe('team');
  await expect(page.locator('.section').filter({ has: page.getByRole('button', { name: 'Collapse Team' }) }).getByRole('button', { name: 'Beta' })).toBeVisible();
});

test('failed bot creation keeps the section form and retries with its placement', async ({ page }) => {
  const sections = [{ id: 'team', name: 'Team', collapsed: false, order: 0 }];
  const agents: Record<string, unknown>[] = [];
  const threads: Record<string, unknown>[] = [];
  const requests: Record<string, unknown>[] = [];
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/agents' && request.method() === 'POST') {
      const body = request.postDataJSON(); requests.push(body);
      if (requests.length === 1) return route.fulfill({ status: 503, json: { message: 'offline' } });
      const agent = { id: 'bot', ...body, active: true, model: '', provider: '' };
      agents.push(agent);
      threads.push({ id: agent.id, agentID: agent.id, agentIDs: [agent.id], title: agent.name, lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID: 'team' });
      return route.fulfill({ json: agent });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Collapse Team' }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'New Bot' }).click();
  const dialog = page.getByRole('dialog', { name: 'New Bot' });
  await dialog.getByRole('textbox', { name: 'Name' }).fill('Beta');
  await dialog.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(dialog.getByText("Couldn't create this bot.")).toBeVisible();
  await expect(dialog.getByRole('textbox', { name: 'Name' })).toHaveValue('Beta');
  await dialog.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(requests.map(request => request.sectionID)).toEqual(['team', 'team']);
  await expect(page.locator('.section').filter({ has: page.getByRole('button', { name: 'Collapse Team' }) }).getByRole('button', { name: 'Beta' })).toBeVisible();
});

test('dragging a section moves it past the target and keyboard moves retain equivalent ordering', async ({ page }) => {
  let sections = [
    { id: 'one', name: 'One', collapsed: false, order: 0 },
    { id: 'two', name: 'Two', collapsed: false, order: 1 },
    { id: 'three', name: 'Three', collapsed: false, order: 2 },
  ];
  const writes: { id: string; beforeID?: string }[] = [];
  let fail = false;
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/sidebar/reorder' && request.method() === 'POST') {
      const body = request.postDataJSON(); writes.push(body);
      if (fail) return route.fulfill({ status: 503, json: { message: 'offline' } });
      const [item] = sections.splice(sections.findIndex(section => section.id === body.id), 1);
      const index = body.beforeID ? sections.findIndex(section => section.id === body.beforeID) : sections.length;
      sections.splice(index, 0, item);
      sections = sections.map((section, order) => ({ ...section, order }));
      return route.fulfill({ json: { id: body.id, order: index } });
    }
    const body = path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const headings = () => page.locator('.section .head').allTextContents().then(items => items.map(item => item.trim()));
  const one = page.getByRole('button', { name: 'Collapse One' });
  const three = page.getByRole('button', { name: 'Collapse Three' });
  await one.dragTo(three);
  await expect.poll(headings).toEqual(['Two', 'Three', 'One']);
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).toEqual({ kind: 'sections', id: 'one' });
  await one.dragTo(page.getByRole('button', { name: 'Collapse Two' }));
  await expect.poll(headings).toEqual(['One', 'Two', 'Three']);
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1]).toEqual({ kind: 'sections', id: 'one', beforeID: 'two' });
  await one.focus();
  await page.keyboard.press('Alt+ArrowDown');
  await expect.poll(headings).toEqual(['Two', 'One', 'Three']);
  await expect.poll(() => writes.length).toBe(3);
  fail = true;
  await one.dragTo(page.getByRole('button', { name: 'Collapse Two' }));
  await expect(page.getByRole('status')).toHaveText('Couldn’t move this section.');
  await expect.poll(headings).toEqual(['Two', 'One', 'Three']);
});
