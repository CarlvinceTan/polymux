import { expect, test, type Page } from '@playwright/test';

const agent = (id: string, name: string) => ({ id, name, role: '', active: true, model: '', provider: '' });
const thread = (id: string, name: string, members = [id]) => ({
  id, agentID: members[0], agentIDs: members, title: name, lastSeq: 0, lastReadSeq: 0, unreadCount: 0, runs: [], sectionID: '',
});

async function menu(page: Page, name: string) {
  const sidebar = page.getByRole('complementary', { name: 'Sidebar' });
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const label = new RegExp(`^(?:(?:Unread, )?Ready, ${escaped}(?:,|$)|${escaped},)`);
  await sidebar.getByRole('button', { name: label }).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete', exact: true }).click();
}

test('bot deletion requires confirmation and cancel sends no request', async ({ page }) => {
  let agents = [agent('alpha', 'Alpha'), agent('beta', 'Beta')];
  let threads = [thread('alpha', 'Alpha'), thread('beta', 'Beta'), thread('group', 'Team', ['alpha', 'beta'])];
  const writes: string[] = [];
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (request.method() === 'DELETE') {
      writes.push(path);
      agents = agents.filter(item => item.id !== 'alpha');
      threads = threads.filter(item => item.id !== 'alpha').map(item => item.id === 'group' ? { ...item, agentID: 'beta', agentIDs: ['beta'] } : item);
      return route.fulfill({ json: { deleted: true } });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await menu(page, 'Alpha');
  let dialog = page.getByRole('alertdialog', { name: 'Delete agent?' });
  await expect(dialog).toContainText('This will permanently remove Alpha and their conversation.');
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(dialog.getByRole('button', { name: 'Delete', exact: true })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  expect(writes).toEqual([]);
  await menu(page, 'Alpha');
  dialog = page.getByRole('alertdialog', { name: 'Delete agent?' });
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  expect(writes).toEqual([]);
  await menu(page, 'Alpha');
  dialog = page.getByRole('alertdialog', { name: 'Delete agent?' });
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Alpha' })).toHaveCount(0);
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Team' })).toBeVisible();
  expect(writes).toEqual(['/agents/alpha']);
});

test('group deletion leaves its bots and uses the thread endpoint', async ({ page }) => {
  const agents = [agent('alpha', 'Alpha'), agent('beta', 'Beta')];
  let threads = [thread('alpha', 'Alpha'), thread('beta', 'Beta'), thread('group', 'Team', ['alpha', 'beta'])];
  const writes: string[] = [];
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (request.method() === 'DELETE') {
      writes.push(path);
      threads = threads.filter(item => item.id !== 'group');
      return route.fulfill({ json: { deleted: true } });
    }
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await menu(page, 'Team');
  const dialog = page.getByRole('alertdialog', { name: 'Delete chat?' });
  await expect(dialog).toContainText('This permanently removes Team and its messages. Its bots stay available.');
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
  const sidebar = page.getByRole('complementary', { name: 'Sidebar' });
  await expect(sidebar.getByRole('button', { name: 'Team' })).toHaveCount(0);
  await expect(sidebar.getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect(sidebar.getByRole('button', { name: 'Beta' })).toBeVisible();
  expect(writes).toEqual(['/threads/group']);
});

test('failed deletion survives refresh and restores only its own chat', async ({ page }) => {
  const agents = [agent('alpha', 'Alpha')];
  const threads = [thread('alpha', 'Alpha')];
  let release!: () => void;
  let requested!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  const started = new Promise<void>(resolve => { requested = resolve; });
  let refreshes = 0;
  await page.route('http://127.0.0.1:8787/**', async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/agents/alpha' && request.method() === 'DELETE') {
      requested();
      await pending;
      return route.fulfill({ status: 503, json: { message: 'offline' } });
    }
    if (path === '/agents' && request.method() === 'POST') {
      const created = agent('beta', request.postDataJSON().name);
      agents.push(created);
      threads.push(thread('beta', created.name));
      return route.fulfill({ json: created });
    }
    if (path === '/sidebar') refreshes++;
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await menu(page, 'Alpha');
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete', exact: true }).click();
  await started;
  const sidebar = page.getByRole('complementary', { name: 'Sidebar' });
  await expect(sidebar.getByRole('button', { name: 'Alpha' })).toHaveCount(0);
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New Bot' }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Beta');
  await page.getByRole('dialog', { name: 'New Bot' }).getByRole('button', { name: 'Create', exact: true }).click();
  await expect(sidebar.getByRole('button', { name: 'Beta' })).toBeVisible();
  await expect.poll(() => refreshes, { timeout: 7000 }).toBeGreaterThan(2);
  await expect(sidebar.getByRole('button', { name: 'Alpha' })).toHaveCount(0);
  release();
  await expect(page.getByRole('status')).toHaveText('Couldn’t delete Alpha.');
  await expect(sidebar.getByRole('button', { name: 'Alpha' })).toBeVisible();
  await expect(sidebar.getByRole('button', { name: 'Beta' })).toBeVisible();
  await expect.poll(() => page.evaluate(() => localStorage.getItem('lastChat.http://127.0.0.1:8787'))).toBe('beta');
});

test('failed group deletion restores the group', async ({ page }) => {
  const agents = [agent('alpha', 'Alpha'), agent('beta', 'Beta')];
  const threads = [thread('alpha', 'Alpha'), thread('beta', 'Beta'), thread('group', 'Team', ['alpha', 'beta'])];
  await page.route('http://127.0.0.1:8787/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith('/draft')) return route.fulfill({ status: 404 });
    if (path === '/threads/group' && request.method() === 'DELETE') return route.fulfill({ status: 503, json: { message: 'offline' } });
    const body = path === '/agents' ? agents : path === '/threads' ? threads
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light', defaultModel: '' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await menu(page, 'Team');
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('Couldn’t delete this chat.');
  await expect(page.getByRole('complementary', { name: 'Sidebar' }).getByRole('button', { name: 'Team' })).toBeVisible();
});
