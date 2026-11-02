import { expect, test } from '@playwright/test';

test.use({ geolocation: { latitude: 51.5074, longitude: -0.1278 }, permissions: ['geolocation'] });

test('location consent reports capability and answers a server request', async ({ page }) => {
  const agent = { id: 'bot', name: 'Bot', role: '', model: '', active: true };
  const capabilityWrites: unknown[] = [];
  const answers: { id: string; body: unknown }[] = [];
  let locationAllowed = false;
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/client' && request.method() === 'PUT') {
      const body = request.postDataJSON();
      capabilityWrites.push(body);
      locationAllowed = Array.isArray(body.capabilities) && body.capabilities.includes('location');
      return route.fulfill({ status: 204 });
    }
    if (path === '/client/requests') {
      return route.fulfill({ contentType: 'text/event-stream', body: locationAllowed ? `data: ${JSON.stringify({ id: 'req-1', capability: 'location', input: '{}' })}\n\n` : '' });
    }
    if (path === '/client/requests/req-1') {
      answers.push({ id: 'req-1', body: request.postDataJSON() });
      return route.fulfill({ status: 204 });
    }
    const data = path === '/agents' ? [agent]
      : path === '/threads' ? [{ id: 'bot', agentID: 'bot', agentIDs: ['bot'], title: 'Bot', lastSeq: 0, unreadCount: 0, runs: [] }]
      : path === '/threads/bot/messages' ? []
      : path === '/sidebar' ? { cursor: 0, sections: [], placements: [] }
      : path === '/settings' ? { appearance: 'system' }
      : path === '/network' ? { host: 'host', current: 'device', devices: [], server: { name: 'Test' } }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: data });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect.poll(() => capabilityWrites.length).toBeGreaterThan(0);
  await page.getByRole('button', { name: 'Location', exact: true }).click();
  await expect.poll(() => capabilityWrites.at(-1)).toEqual({ platform: 'macos', timezone: expect.any(String), capabilities: ['location'] });
  await expect.poll(() => answers.length).toBe(1);
  const reading = JSON.parse((answers[0].body as { output: string }).output);
  expect(reading.latitude).toBeCloseTo(51.5074, 4);
  expect(reading.longitude).toBeCloseTo(-0.1278, 4);
  await page.getByRole('button', { name: 'Location', exact: true }).click();
  await expect.poll(() => capabilityWrites.at(-1)).toEqual({ platform: 'macos', timezone: expect.any(String), capabilities: [] });
});
