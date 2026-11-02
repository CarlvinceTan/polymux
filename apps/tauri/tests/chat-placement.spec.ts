import { expect, test, type Page, type Route } from '@playwright/test';

const sections = [{ id: 'one', name: 'One', collapsed: false, order: 0 }, { id: 'two', name: 'Two', collapsed: false, order: 1 }];
const agent = (id: string, name: string) => ({ id, name, role: '', active: true, model: '', provider: '' });
const thread = (id: string, name: string, sectionID?: string) => ({ id, agentID: id, agentIDs: [id], title: name, lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID, sectionOrder: 0, pinned: false, pinOrder: undefined as number | undefined });

async function choose(page: Page, name: string, choice: string) {
  await page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: choice, exact: true }).click();
}

async function connect(page: Page, handle: (route: Route) => Promise<void>) {
  await page.route('http://127.0.0.1:8787/**', handle);
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
}

test('pin, unpin and move use placement semantics including explicit Unassigned', async ({ page }) => {
  const agents = [agent('alpha', 'Alpha'), agent('beta', 'Beta')];
  const threads = [thread('alpha', 'Alpha', 'one'), thread('beta', 'Beta', 'two')];
  const writes: { id: string; body: Record<string, unknown> }[] = [];
  await connect(page, async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    const match = path.match(/^\/threads\/(.+)\/placement$/);
    if (match && request.method() === 'PATCH') {
      const body = request.postDataJSON();
      writes.push({ id: match[1], body });
      const item = threads.find(value => value.id === match[1])!;
      item.pinned = body.pinned;
      item.pinOrder = body.pinned ? threads.filter(value => value.pinned && value !== item).length : undefined;
      item.sectionID = body.pinned ? undefined : body.sectionID ?? undefined;
      item.sectionOrder = body.pinned ? undefined : 0;
      return route.fulfill({ json: item });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await choose(page, 'Beta', 'Pin');
  await expect(page.locator('.pins').getByRole('button', { name: 'Beta' })).toBeVisible();
  await choose(page, 'Alpha', 'Pin');
  await expect(page.locator('.pins').getByRole('button')).toHaveText(['Beta', 'Alpha']);
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1]).toEqual({ id: 'alpha', body: { pinned: true, afterID: 'beta' } });
  await choose(page, 'Alpha', 'Move to Two');
  await expect(page.locator('[data-section="two"]').getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect.poll(() => writes.length).toBe(3);
  expect(writes[2]).toEqual({ id: 'alpha', body: { pinned: false, sectionID: 'two' } });
  await choose(page, 'Alpha', 'Move to Unassigned');
  await expect(page.locator('[data-section="__unassigned__"]').getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect.poll(() => writes.length).toBe(4);
  expect(writes[3]).toEqual({ id: 'alpha', body: { pinned: false, sectionID: null } });
  await choose(page, 'Beta', 'Unpin');
  await expect(page.locator('[data-section="__unassigned__"]').getByRole('button', { name: 'Beta' })).toBeVisible();
  await expect.poll(() => writes.length).toBe(5);
  expect(writes[4]).toEqual({ id: 'beta', body: { pinned: false, sectionID: null } });
});

test('failed move survives refresh and rollback keeps a concurrently created bot', async ({ page }) => {
  const agents = [agent('alpha', 'Alpha')];
  const threads = [thread('alpha', 'Alpha', 'one')];
  let release!: () => void;
  let requested!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  const started = new Promise<void>(resolve => { requested = resolve; });
  let refreshes = 0;
  await connect(page, async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/threads/alpha/placement' && request.method() === 'PATCH') {
      requested(); await pending;
      return route.fulfill({ status: 400, json: { message: 'Move refused.' } });
    }
    if (path === '/agents' && request.method() === 'POST') {
      const created = agent('beta', request.postDataJSON().name);
      agents.push(created); threads.push(thread('beta', created.name));
      return route.fulfill({ json: created });
    }
    if (path === '/sidebar') refreshes++;
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await choose(page, 'Alpha', 'Move to Two');
  await started;
  await expect(page.locator('[data-section="two"]').getByRole('button', { name: 'Alpha' })).toBeVisible();
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New Bot' }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Beta');
  await page.getByRole('dialog', { name: 'New Bot' }).getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Beta' })).toBeVisible();
  await expect.poll(() => refreshes, { timeout: 7000 }).toBeGreaterThan(2);
  await expect(page.locator('[data-section="two"]').getByRole('button', { name: 'Alpha' })).toBeVisible();
  release();
  await expect(page.getByRole('status')).toHaveText('Move refused.');
  await expect(page.locator('[data-section="one"]').getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Beta' })).toBeVisible();
});

test('a failed pin rolls back and retries after a successful refresh', async ({ page }) => {
  const agents = [agent('alpha', 'Alpha')];
  const threads = [thread('alpha', 'Alpha', 'one')];
  let attempts = 0;
  await connect(page, async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/threads/alpha/placement' && request.method() === 'PATCH') {
      attempts++;
      if (attempts === 1) return route.fulfill({ status: 503, json: { message: 'offline' } });
      threads[0] = { ...threads[0], pinned: true, pinOrder: 0, sectionID: undefined, sectionOrder: undefined };
      return route.fulfill({ json: threads[0] });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await choose(page, 'Alpha', 'Pin');
  await expect(page.getByRole('status')).toHaveText('Couldn’t save that change yet. It will try again.');
  await expect(page.locator('[data-section="one"]').getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect.poll(() => attempts, { timeout: 7000 }).toBe(2);
  await expect(page.locator('.pins').getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect(page.getByRole('status')).toHaveCount(0);
});
