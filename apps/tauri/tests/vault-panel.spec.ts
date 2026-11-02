import { expect, test } from '@playwright/test';

test('vault settings browse, filter, create, edit, and import entries', async ({ page }) => {
  let items = [
    { id: 'login-1', kind: 'login', title: 'Example login', origin: 'https://example.com', fields: ['username', 'password'], revision: 1 },
    { id: 'note-1', kind: 'note', title: 'Recovery note', origin: '', fields: ['notes'], revision: 1 },
  ];
  const writes: unknown[] = [];
  let importedBody = '';
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/vault') return route.fulfill({ json: { created: true, available: true, protection: 'keychain' } });
    if (path === '/vault/items' && request.method() === 'GET') return route.fulfill({ json: items });
    if (path === '/vault/items/login-1' && request.method() === 'GET') {
      return route.fulfill({ json: { id: 'login-1', kind: 'login', title: 'Example login', url: 'https://example.com', fields: { username: '', password: '' }, codes: [], revision: 1, removed: [] } });
    }
    if (path.startsWith('/vault/items/') && request.method() === 'PUT') {
      const body = request.postDataJSON();
      writes.push(body);
      const summary = { id: body.id, kind: body.kind, title: body.title, origin: body.url, fields: Object.keys(body.fields), revision: 1 };
      items = [...items.filter((item) => item.id !== body.id), summary];
      return route.fulfill({ json: { ...body, revision: 1 } });
    }
    if (path === '/vault/import') {
      importedBody = request.postData() ?? '';
      return route.fulfill({ json: { added: 3, skipped: 1 } });
    }
    const data = path === '/agents' ? []
      : path === '/threads' ? []
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path === '/network' ? { devices: [] } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Vault', exact: true }).click();
  await expect(page.getByLabel('Vault item count')).toHaveText('2 items');
  await expect(page.getByRole('button', { name: 'Example login' })).toBeVisible();

  await page.getByLabel('Search vault').fill('recovery');
  await expect(page.getByLabel('Vault item count')).toHaveText('1 result');
  await page.getByLabel('Search vault').fill('');
  await page.getByRole('button', { name: 'Notes', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Recovery note' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Example login' })).toHaveCount(0);
  await page.getByRole('button', { name: 'All', exact: true }).click();

  await page.getByRole('button', { name: 'Example login' }).click();
  await page.getByLabel('Vault title').fill('Updated login');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Updated login' })).toBeVisible();
  expect(writes[0]).toMatchObject({ id: 'login-1', title: 'Updated login' });

  await page.getByRole('button', { name: 'Add vault item' }).click();
  await page.getByRole('menuitem', { name: 'Login' }).click();
  await page.getByLabel('Vault title').fill('New login');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'New login' })).toBeVisible();

  await page.getByRole('button', { name: 'Add vault item' }).click();
  await page.getByRole('menuitem', { name: 'Import' }).click();
  await page.getByLabel('Choose vault import').setInputFiles({ name: 'vault.csv', mimeType: 'text/csv', buffer: Buffer.from('name,url\nExample,https://example.com\n') });
  await page.getByRole('button', { name: 'Import', exact: true }).click();
  await expect(page.getByText('3 imported · 1 already present')).toBeVisible();
  if (importedBody) expect(importedBody).toContain('Example');
});
