import { expect, test } from '@playwright/test';

for (const colorScheme of ['light', 'dark'] as const) {
test(`devices show their real identity, rename, and roll back a failed removal (${colorScheme})`, async ({ page }) => {
  await page.emulateMedia({ colorScheme });
  const writes: { method: string; path: string; body?: unknown }[] = [];
  await page.route('http://127.0.0.1:8787/**', async (route) => {
    const request = route.request();
    const { pathname } = new URL(request.url());
    if (pathname.startsWith('/network/devices/')) {
      writes.push({ method: request.method(), path: pathname, body: request.postData() ? request.postDataJSON() : undefined });
      return request.method() === 'DELETE' ? route.fulfill({ status: 500, json: { message: 'offline' } }) : route.fulfill({ status: 204 });
    }
    const body = pathname === '/network' ? {
      current: 'phone', host: 'host', online: ['phone', 'host'], server: { name: 'Server' },
      devices: [{ id: 'host', name: 'Mac' }, { id: 'phone', name: 'Phone' }, { id: 'tablet', name: 'Tablet' }],
    } : pathname === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : pathname === '/settings' ? { appearance: 'system' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Phone', exact: true })).toBeVisible();
  await expect(page.getByText('This device', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Remove Mac' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Remove Phone' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Phone', exact: true }).click();
  await page.getByRole('textbox', { name: 'Name' }).fill('My phone');
  await page.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByRole('button', { name: 'My phone', exact: true })).toBeVisible();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0]).toEqual({ method: 'PATCH', path: '/network/devices/phone', body: { label: 'My phone' } });
  await page.getByRole('button', { name: 'Remove Tablet' }).click();
  await expect(page.getByRole('status')).toContainText("Couldn't remove Tablet.");
  await expect(page.getByRole('button', { name: 'Tablet', exact: true })).toBeVisible();
});
}
