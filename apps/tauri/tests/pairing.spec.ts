import { expect, test } from '@playwright/test';
import jsQR from 'jsqr';

test('add device shows an invite and joins a pasted link', async ({ page }) => {
  const invite = { url: 'https://joined.example', token: 'one-time' };
  const received: unknown[] = [];
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: async (value: string) => { (window as any).copied = value; } } });
  });
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = path === '/network/invite' ? invite
      : path === '/network' ? { devices: [], server: { name: 'Local' } }
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'system' } : [];
    return route.fulfill({ json: body });
  });
  await page.route('https://joined.example/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/network/pair') {
      received.push(request.postDataJSON());
      return route.fulfill({ status: 204 });
    }
    const body = path === '/agents' ? [{ id: 'remote', name: 'Remote bot', role: '', model: '', active: true }]
      : path === '/threads' ? [{ id: 'remote', agentID: 'remote', agentIDs: ['remote'], title: 'Remote bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'system' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.getByRole('button', { name: 'Add device', exact: true }).click();
  const qr = page.getByRole('img', { name: 'Pairing QR code' });
  await expect(qr).toBeVisible();
  await page.getByRole('button', { name: 'Copy link' }).click();
  const link = await page.evaluate(() => (window as any).copied as string);
  expect(link).toMatch(/^polymux:\/\/connect\?/);
  const pixels = await qr.evaluate(async (element) => {
    const svg = element.cloneNode(true) as SVGElement;
    svg.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
    const src = URL.createObjectURL(new Blob([new XMLSerializer().serializeToString(svg)], { type: 'image/svg+xml' }));
    try {
      const image = new Image();
      image.src = src;
      await image.decode();
      const canvas = document.createElement('canvas');
      canvas.width = 220; canvas.height = 220;
      const context = canvas.getContext('2d')!;
      context.fillStyle = 'white'; context.fillRect(0, 0, 220, 220);
      context.imageSmoothingEnabled = false;
      context.drawImage(image, 12, 12, 196, 196);
      return Array.from(context.getImageData(0, 0, 220, 220).data);
    } finally { URL.revokeObjectURL(src); }
  });
  expect(jsQR(new Uint8ClampedArray(pixels), 220, 220)?.data).toBe(link);
  const field = page.getByRole('textbox', { name: 'Pairing link' });
  await field.fill('not a link');
  await page.getByRole('button', { name: 'Join server' }).click();
  await expect(page.getByRole('status')).toHaveText('Paste a valid Polymux pairing link.');
  await field.fill(link);
  await page.getByRole('button', { name: 'Join server' }).click();
  await expect(page.getByRole('button', { name: 'Remote bot' }).first()).toBeVisible();
  expect(received).toHaveLength(1);
  expect((received[0] as { code: string }).code).toBe(invite.token);
  expect(await page.evaluate(() => localStorage.getItem('polymux.server'))).toBe(invite.url);
});
