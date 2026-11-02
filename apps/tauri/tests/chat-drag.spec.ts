import { expect, test, type Page, type Route } from '@playwright/test';

const sections = [{ id: 'one', name: 'One', collapsed: false, order: 0 }, { id: 'two', name: 'Two', collapsed: false, order: 1 }];
const agent = (id: string, name: string) => ({ id, name, role: '', active: true, model: '', provider: '' });
const thread = (id: string, name: string, sectionID?: string, pinOrder?: number) => ({
  id, agentID: id, agentIDs: [id], title: name, lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [],
  sectionID, sectionOrder: sectionID ? 0 : undefined, pinned: pinOrder !== undefined, pinOrder,
});

async function connect(page: Page, handler: (route: Route) => Promise<void>) {
  await page.route('http://127.0.0.1:8787/**', handler);
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
}

test('dragging chats targets sections and inserts pins before a tile', async ({ page }) => {
  const agents = [agent('alpha', 'Alpha'), agent('beta', 'Beta'), agent('gamma', 'Gamma'), agent('delta', 'Delta'), agent('epsilon', 'Epsilon'), agent('zeta', 'Zeta'), agent('other', 'Other')];
  const threads = [thread('alpha', 'Alpha', 'one'), thread('beta', 'Beta', 'two'), thread('gamma', 'Gamma', undefined, 0), thread('delta', 'Delta', undefined, 1), thread('epsilon', 'Epsilon', undefined, 2), thread('zeta', 'Zeta', undefined, 3), thread('other', 'Other')];
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
      if (body.pinned) {
        const pins = threads.filter(value => value.pinned && value !== item).sort((a, b) => a.pinOrder! - b.pinOrder!);
        const index = body.beforeID ? pins.findIndex(value => value.id === body.beforeID) : pins.length;
        pins.splice(index, 0, item);
        pins.forEach((value, order) => { value.pinned = true; value.pinOrder = order; value.sectionID = undefined; value.sectionOrder = undefined; });
      } else {
        item.pinned = false; item.pinOrder = undefined; item.sectionID = body.sectionID ?? undefined; item.sectionOrder = 1;
      }
      return route.fulfill({ json: item });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  const row = (name: string) => page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name });
  const pinNames = () => page.locator('.pins .pin-title').allTextContents();
  await row('Alpha').dragTo(page.getByRole('button', { name: 'Collapse Two' }));
  await expect(page.locator('[data-section="two"]').getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).toEqual({ id: 'alpha', body: { pinned: false, sectionID: 'two' } });
  await row('Alpha').dragTo(page.locator('.pins').getByRole('button', { name: 'Gamma' }));
  await expect.poll(pinNames).toEqual(['Alpha', 'Gamma', 'Delta', 'Epsilon', 'Zeta']);
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1]).toEqual({ id: 'alpha', body: { pinned: true, beforeID: 'gamma' } });
  await row('Delta').dragTo(page.locator('.pins').getByRole('button', { name: 'Gamma' }));
  await expect.poll(pinNames).toEqual(['Alpha', 'Delta', 'Gamma', 'Epsilon', 'Zeta']);
  await expect.poll(() => writes.length).toBe(3);
  expect(writes[2]).toEqual({ id: 'delta', body: { pinned: true, beforeID: 'gamma' } });
  await row('Beta').dragTo(page.locator('.pins').getByRole('button', { name: 'Epsilon' }));
  await expect.poll(pinNames).toEqual(['Alpha', 'Delta', 'Gamma', 'Beta', 'Epsilon', 'Zeta']);
  await expect.poll(() => writes.length).toBe(4);
  expect(writes[3]).toEqual({ id: 'beta', body: { pinned: true, beforeID: 'epsilon' } });
  await row('Alpha').dragTo(page.getByRole('button', { name: 'Collapse One' }));
  await expect(page.locator('[data-section="one"]').getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect.poll(() => writes.length).toBe(5);
  expect(writes[4]).toEqual({ id: 'alpha', body: { pinned: false, sectionID: 'one' } });
  await row('Alpha').dragTo(page.getByRole('button', { name: 'Collapse One' }));
  expect(writes).toHaveLength(5);
});

test('empty Pinned and Unassigned targets appear during a chat drag', async ({ page }) => {
  const agents = [agent('alpha', 'Alpha')];
  const threads = [thread('alpha', 'Alpha', 'one')];
  const writes: Record<string, unknown>[] = [];
  await connect(page, async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/threads/alpha/placement' && request.method() === 'PATCH') {
      const body = request.postDataJSON(); writes.push(body);
      threads[0] = { ...threads[0], pinned: true, pinOrder: 0, sectionID: undefined, sectionOrder: undefined };
      return route.fulfill({ json: threads[0] });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections, placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  const row = page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Alpha' });
  const source = await row.boundingBox();
  if (!source) throw new Error('Missing source');
  await page.mouse.move(source.x + source.width / 2, source.y + source.height / 2);
  await page.mouse.down();
  await page.mouse.move(source.x + source.width / 2, source.y + source.height / 2 + 10, { steps: 5 });
  await expect(page.locator('.pins')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Collapse Unassigned' })).toBeVisible();
  const target = await page.locator('.pins').boundingBox();
  if (!target) throw new Error('Missing Pinned target');
  await page.mouse.move(target.x + target.width / 2, target.y + target.height / 2, { steps: 5 });
  await page.mouse.up();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).toEqual({ pinned: true });
  await expect(page.locator('.pins').getByRole('button', { name: 'Alpha' })).toBeVisible();
});

test('a failed pin insertion restores pin ordering without reverting unrelated changes', async ({ page }) => {
  const agents = [agent('alpha', 'Alpha'), agent('gamma', 'Gamma'), agent('delta', 'Delta')];
  const threads = [thread('alpha', 'Alpha', 'one'), thread('gamma', 'Gamma', undefined, 0), thread('delta', 'Delta', undefined, 1)];
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
      return route.fulfill({ status: 400, json: { message: 'Pin refused.' } });
    }
    if (path === '/threads/gamma/placement' && request.method() === 'PATCH') {
      const item = threads.find(value => value.id === 'gamma')!;
      item.pinned = false; item.pinOrder = undefined; item.sectionID = 'one'; item.sectionOrder = 1;
      return route.fulfill({ json: item });
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
  const row = (name: string) => page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name });
  const pinNames = () => page.locator('.pins .pin-title').allTextContents();
  await row('Alpha').dragTo(row('Gamma'));
  await started;
  await expect.poll(pinNames).toEqual(['Alpha', 'Gamma', 'Delta']);
  await row('Gamma').dragTo(page.getByRole('button', { name: 'Collapse One' }));
  await expect(page.locator('[data-section="one"]').getByRole('button', { name: 'Gamma' })).toBeVisible();
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New Bot' }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Beta');
  await page.getByRole('dialog', { name: 'New Bot' }).getByRole('button', { name: 'Create', exact: true }).click();
  await expect(row('Beta')).toBeVisible();
  const beforeRefresh = refreshes;
  await expect.poll(() => refreshes, { timeout: 7000 }).toBeGreaterThan(beforeRefresh);
  await expect.poll(pinNames).toEqual(['Alpha', 'Delta']);
  release();
  await expect(page.getByRole('status')).toHaveText('Pin refused.');
  await expect.poll(pinNames).toEqual(['Delta']);
  await expect(page.locator('[data-section="one"]').getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect(page.locator('[data-section="one"]').getByRole('button', { name: 'Gamma' })).toBeVisible();
  await expect(row('Beta')).toBeVisible();
});
