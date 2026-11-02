import { expect, test } from '@playwright/test';

test('thinking follows the current bot run and stops for streamed text', async ({ page }) => {
  await page.goto('/');
  const actual = await page.evaluate(async () => {
    const { thinking } = await import('/lib/avatar/activity.ts');
    const run = { id: 'r', agentID: 'a', conversationID: 'chat', status: 'running', activity: 'generating', trigger: 'user', createdAt: '2026-01-01T00:00:00Z', seq: 1 };
    const status = (runs: any[], messages: any[] = [], agent = 'a') => thinking(runs, messages, 'chat', agent);
    return [
      status([run]),
      status([run], [], 'b'),
      status([{ ...run, activity: 'tool' }]),
      status([{ ...run, trigger: 'created' }]),
      status([run], [{ runID: 'r', status: 'streaming', parts: [{ text: 'Hello' }] }]),
      status([run], [{ runID: 'other', status: 'streaming', parts: [{ text: 'Hello' }] }]),
      status([run, { ...run, id: 'wait', status: 'waiting_input', createdAt: '2026-01-02T00:00:00Z' }]),
      status([run, { ...run, id: 'done', status: 'completed', createdAt: '2026-01-02T00:00:00Z' }]),
    ];
  });
  expect(actual).toEqual([true, false, false, false, false, true, false, true]);
});

test('thinking dots match the reference reduced-motion frame and settle when activity ends', async ({ page }) => {
  await page.goto('/');
  const actual = await page.evaluate(async () => {
    const { thinkingDot, thinkingZoom, ThinkingMotion } = await import('/lib/avatar/kit/thinking.ts');
    const motion = new ThinkingMotion();
    motion.step(true, 0, true);
    const core = thinkingDot(1, 1, motion.amount, true);
    const dot = thinkingDot(0, 1, motion.amount, true);
    const zoom = thinkingZoom(52, motion.amount);
    const geometry = [core.radius * 100 * zoom, dot.x * 100 * zoom, dot.radius * 1.02 * 100 * zoom];
    for (let i = 0; i < 60; i++) motion.step(false, 1 / 60, false);
    return { geometry, amount: motion.amount };
  });
  // BotEngine.sample(1) with thinking mood, reduced motion and 52-point display size.
  const reference = [28.664187662095, -80.78089250226772, 29.2374714153369];
  actual.geometry.forEach((value, index) => expect(value).toBeCloseTo(reference[index], 8));
  expect(actual.amount).toBe(0);
});

test('round avatars show the thinking pose and honor reduced motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.route('http://127.0.0.1:8787/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    const agents = ['busy', 'idle'].map((id) => ({ id, name: id, role: '', model: '', active: true }));
    const run = { id: 'run', agentID: 'busy', conversationID: 'busy', status: 'running', activity: 'generating', trigger: 'user', createdAt: '2026-01-01T00:00:00Z', seq: 1 };
    const body = path === '/agents' ? agents
      : path === '/threads' ? agents.map((agent) => ({ id: agent.id, agentID: agent.id, agentIDs: [agent.id], title: agent.name, lastSeq: 1, unreadCount: 0, runs: agent.id === 'busy' ? [run] : [] }))
      : path === '/sidebar' ? { sections: [], placements: [], cursor: 0 }
      : path === '/settings' ? { appearance: 'light' }
      : path.endsWith('/draft') ? { revision: 0, writeID: '', value: { text: '', mentions: [] } } : [];
    return route.fulfill({ json: body });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  const busy = page.getByRole('button', { name: /busy/ }).locator('canvas').first();
  const idle = page.getByRole('button', { name: /idle/ }).locator('canvas').first();
  await expect(busy).toBeVisible();
  const pixels = (canvas: import('@playwright/test').Locator) => canvas.evaluate((element: HTMLCanvasElement) => {
    const data = element.getContext('2d')!.getImageData(0, 0, element.width, element.height).data;
    return [...data].filter((_, index) => index % 4 === 3 && data[index] > 127).length;
  });
  await expect.poll(() => pixels(busy)).toBeLessThan(await pixels(idle));
  const fixed = await pixels(busy);
  await page.waitForTimeout(300);
  expect(await pixels(busy)).toBe(fixed);
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const image = () => busy.evaluate((element: HTMLCanvasElement) => element.toDataURL());
  const initial = await image();
  await expect.poll(image).not.toBe(initial);
});
