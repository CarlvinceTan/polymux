import { expect, test } from '@playwright/test';

test('native saves use bounded chunks and clean up after cancellation or failure', async ({ page }) => {
  await page.goto('/avatar-test.html');
  const result = await page.evaluate(async () => {
    let mode = 'cancel';
    let aborter: AbortController;
    const calls: { command: string; id?: number; commit?: boolean; name?: string; length?: number; first?: number; last?: number; header?: string }[] = [];
    Object.assign(window, { __TAURI_INTERNALS__: {
      invoke: async (command: string, args: ArrayBuffer | { name?: string; id?: number; commit?: boolean }, options?: { headers: Record<string, string> }) => {
        if (command === 'start_save') {
          calls.push({ command, name: (args as { name: string }).name });
          return mode === 'cancel' ? null : 17;
        }
        if (command === 'finish_save') {
          const { id, commit } = args as { id: number; commit: boolean };
          calls.push({ command, id, commit });
          return;
        }
        const bytes = new Uint8Array(args as ArrayBuffer);
        calls.push({ command, length: bytes.length, first: bytes[0], last: bytes[bytes.length - 1], header: options?.headers['x-resource-id'] });
        if (mode === 'failure') throw new Error('write failed');
        if (mode === 'abort') aborter.abort();
      },
    } });
    const { saveMedia } = await import('/lib/shell/saveMedia.ts');
    const cancelled = await saveMedia(new Blob(['x']), 'résumé.txt');
    mode = 'success';
    const data = new Uint8Array(2 * 1024 * 1024 + 3);
    data[0] = 11; data[data.length - 1] = 99;
    const saved = await saveMedia(new Blob([data]), 'résumé.txt');
    mode = 'failure';
    let failed = false;
    try { await saveMedia(new Blob(['x']), 'fail.txt'); } catch { failed = true; }
    mode = 'abort';
    aborter = new AbortController();
    let aborted = false;
    try { await saveMedia(new Blob([data]), 'cancel.txt', aborter.signal); }
    catch (error) { aborted = (error as Error).name === 'AbortError'; }
    return { calls, cancelled, saved, failed, aborted };
  });
  expect(result.cancelled).toBe(false);
  expect(result.saved).toBe(true);
  expect(result.failed).toBe(true);
  expect(result.aborted).toBe(true);
  expect(result.calls).toEqual([
    { command: 'start_save', name: 'résumé.txt' },
    { command: 'start_save', name: 'résumé.txt' },
    { command: 'save_chunk', length: 1024 * 1024, first: 11, last: 0, header: '17' },
    { command: 'save_chunk', length: 1024 * 1024, first: 0, last: 0, header: '17' },
    { command: 'save_chunk', length: 3, first: 0, last: 99, header: '17' },
    { command: 'finish_save', id: 17, commit: true },
    { command: 'start_save', name: 'fail.txt' },
    { command: 'save_chunk', length: 1, first: 120, last: 120, header: '17' },
    { command: 'finish_save', id: 17, commit: false },
    { command: 'start_save', name: 'cancel.txt' },
    { command: 'save_chunk', length: 1024 * 1024, first: 11, last: 0, header: '17' },
    { command: 'finish_save', id: 17, commit: false },
  ]);
});
