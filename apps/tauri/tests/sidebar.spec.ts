import { expect, test } from '@playwright/test';

for (const colorScheme of ['light', 'dark'] as const) {
  for (const viewport of [{ width: 780, height: 720 }, { width: 1440, height: 900 }]) {
  test(`sidebar drag, collapse and restore (${colorScheme}, ${viewport.width}x${viewport.height})`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await page.emulateMedia({ colorScheme });
    await page.route('http://127.0.0.1:8787/**', (route) => {
      const path = new URL(route.request().url()).pathname;
      const body = path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
        : path === '/settings' ? { appearance: 'system' } : [];
      return route.fulfill({ json: body });
    });
    await page.goto('/');
    await page.getByRole('button', { name: 'Connect', exact: true }).click();
    const splitter = page.getByRole('separator', { name: 'Sidebar width' });
    const sidebar = page.getByRole('complementary', { name: 'Sidebar' });
    const reveal = page.getByRole('button', { name: 'Show sidebar' });
    await expect(splitter).toBeVisible();
    const frame = (await splitter.boundingBox())!;
    const start = frame.x + frame.width / 2;
    const middle = frame.y + frame.height / 2;
    await page.mouse.move(start, middle);
    await page.mouse.down();
    await page.mouse.move(start + 60, middle);
    await expect(splitter).toHaveAttribute('aria-valuenow', '380');
    await expect(sidebar).toHaveCSS('width', '380px');
    await page.mouse.move(start - 150, middle);
    await expect(splitter).toHaveAttribute('aria-valuenow', '260');
    await page.mouse.move(start - 220, middle);
    await expect(sidebar).toHaveCSS('width', '0px');
    await page.mouse.move(start + 100, middle);
    await expect(sidebar).toHaveCSS('width', '0px');
    await page.mouse.up();
    await expect(reveal).toBeVisible();
    await reveal.click();
    await expect(sidebar).toHaveCSS('width', '260px');
    await expect(page.getByRole('button', { name: 'Settings', exact: true })).toBeVisible();

    await splitter.focus();
    await page.keyboard.press('End');
    await page.keyboard.press('Enter');
    await expect(reveal).toBeVisible();
    await reveal.click();
    await expect(sidebar).toHaveCSS('width', '400px');
    await expect(reveal).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  });
}
}

