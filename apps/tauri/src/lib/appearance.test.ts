import assert from 'node:assert/strict';
import test from 'node:test';

test('one system listener follows system changes without changing the saved preference', async () => {
  let current = 'system';
  let callback: ((event: { matches: boolean }) => void) | undefined;
  let listeners = 0;
  const media = { matches: false, addEventListener: (_type: string, listener: typeof callback) => { listeners++; callback = listener; } };
  Object.assign(globalThis, {
    window: { matchMedia: () => media },
    document: { documentElement: { dataset: { mode: 'light' } } },
    localStorage: { getItem: () => current, setItem: (_key: string, value: string) => { current = value; } },
  });
  const { applyAppearance } = await import('./appearance');
  const root = document.documentElement;
  const change = (dark: boolean) => { media.matches = dark; callback!({ matches: dark }); };
  applyAppearance('system');
  change(true);
  assert.equal(root.dataset.mode, 'dark');
  assert.equal(current, 'system');
  applyAppearance('light');
  change(false);
  change(true);
  assert.equal(root.dataset.mode, 'light');
  assert.equal(current, 'light');
  applyAppearance('system');
  assert.equal(root.dataset.mode, 'dark');
  change(false);
  assert.equal(root.dataset.mode, 'light');
  assert.equal(current, 'system');
  applyAppearance('dark', false);
  change(false);
  assert.equal(root.dataset.mode, 'dark');
  assert.equal(current, 'system');
  assert.equal(listeners, 1);
});
