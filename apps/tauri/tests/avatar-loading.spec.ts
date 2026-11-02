import { expect, test } from '@playwright/test';

test('a pending avatar retains the prior image and late loads cannot replace a newer choice', async ({ page }) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route('**/lib/avatar/kit/whale.json*', async (route) => { await gate; await route.continue(); });
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light' } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'New Bot', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'New Bot' });
  await dialog.getByRole('button', { name: 'Animals', exact: true }).click();
  const canvas = dialog.locator('.preview canvas');
  const color = (rgb: number[]) => canvas.evaluate((element: HTMLCanvasElement, expected) => {
    const data = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
    for (let i = 0; i < data.length; i += 4) if (data[i] === expected[0] && data[i + 1] === expected[1] && data[i + 2] === expected[2]) return true;
    return false;
  }, rgb);
  await dialog.getByRole('button', { name: 'Cat', exact: true }).click();
  await expect.poll(() => color([135, 150, 172])).toBe(true);
  const old = await canvas.evaluate((element: HTMLCanvasElement) => element.toDataURL());
  await dialog.getByRole('button', { name: 'Whale', exact: true }).click();
  await expect.poll(() => canvas.evaluate((element: HTMLCanvasElement) => element.toDataURL())).toBe(old);
  await dialog.getByRole('button', { name: 'Dog', exact: true }).click();
  await expect.poll(() => color([195, 148, 96])).toBe(true);
  const dog = await canvas.evaluate((element: HTMLCanvasElement) => element.toDataURL());
  release();
  await expect.poll(() => dialog.getByRole('button', { name: 'Whale', exact: true }).locator('canvas').evaluate((element: HTMLCanvasElement) => {
    const data = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
    for (let i = 0; i < data.length; i += 4) if (data[i] === 77 && data[i + 1] === 107 && data[i + 2] === 254) return true;
    return false;
  })).toBe(true);
  expect(await canvas.evaluate((element: HTMLCanvasElement) => element.toDataURL())).toBe(dog);
  await dialog.getByRole('button', { name: 'Whale', exact: true }).click();
  await expect.poll(() => color([77, 107, 254])).toBe(true);
});
