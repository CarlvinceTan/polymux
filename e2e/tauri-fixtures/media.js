// Run with scripts/run-e2e.sh --tauri --verify-server e2e/tauri-fixtures/media.js
const app = await cua.getApp(APP);
const main = (await cua.listWindows({app: APP}))[0];
if (!main) throw new Error('Could not find the app window');
const pid = main.pid;
const mainApp = await cua.getApp({pid, windowId: main.window_id});
await app.waitFor({role: 'button', name: 'Search', timeout_ms: 10000});
let rows = [];
for (let i = 0; i < 40 && rows.length !== 1; i++) {
  await cua.sleep(250);
  rows = (await app.query({role: 'button'})).filter((row) => row.label?.includes('Media,'));
}
if (rows.length !== 1) throw new Error('Could not identify the Media sidebar row');
await app.click(rows[0].element_token);
await app.waitFor({role: 'button', name: 'View pixel.png', timeout_ms: 10000});
await app.click({role: 'button', name: 'View pixel.png'});
await app.waitFor({role: 'button', name: 'Quick Look', timeout_ms: 5000});
await app.waitFor({role: 'button', name: 'Open'});
await app.waitFor({role: 'button', name: 'Share'});
await app.waitFor({role: 'button', name: 'Save…'});

for (let i = 0; i < 40; i++) {
  const [quick] = await app.query({role: 'button', name: 'Quick Look'});
  if (quick?.enabled) break;
  if (i === 39) throw new Error('Quick Look stayed disabled');
  await cua.sleep(250);
}

await app.click({role: 'button', name: 'Quick Look'});
let panel;
let preview;
for (let i = 0; i < 40 && !panel; i++) {
  await cua.sleep(200);
  for (const window of (await cua.listWindows({pid})).filter((candidate) => candidate.layer > 0)) {
    try {
      const candidate = await cua.getApp({pid, windowId: window.window_id});
      const state = await candidate.getState({include_screenshot: false});
      if (state.tree_markdown?.includes('Open with Preview')) {
        panel = window;
        preview = candidate;
        break;
      }
    } catch {}
  }
}
if (!panel) throw new Error('Quick Look did not open');
await preview.pressKey('escape');
for (let i = 0; i < 20; i++) {
  await cua.sleep(200);
  if (!(await cua.listWindows({pid})).some((window) => window.layer > 0)) break;
}

await app.click({role: 'button', name: 'Open'});
let opened;
for (let i = 0; i < 30 && !opened; i++) {
  await cua.sleep(200);
  opened = (await cua.listWindows({app: 'Preview'})).find((window) => window.title === 'pixel.png');
}
if (!opened) throw new Error('Open did not launch a Preview window');
const openedApp = await cua.getApp({pid: opened.pid, windowId: opened.window_id});
await openedApp.hotkey(['cmd', 'w']);

await app.click({role: 'button', name: 'Share'});
let sheet;
for (let i = 0; i < 30 && !sheet; i++) {
  await cua.sleep(200);
  sheet = (await cua.listWindows({pid})).find((window) => window.layer > 0);
}
if (!sheet) throw new Error('Share did not open a sharing sheet');
await mainApp.pressKey('escape');
await cua.sleep(500);
const [close] = await mainApp.query({role: 'button', name: 'Close preview'});
if (close) await mainApp.click(close.element_token);
return 'ok';
