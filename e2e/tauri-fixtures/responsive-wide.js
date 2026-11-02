// @size 1440x900
// Run with scripts/run-e2e.sh --verify-server e2e/tauri-fixtures/responsive-wide.js
const app = await cua.getApp(APP);
let window;
for (let i = 0; i < 50; i++) {
  window = (await cua.listWindows({app: APP}))[0];
  if (window && Math.abs(window.bounds.width - 1440) <= 2 && Math.abs(window.bounds.height - 900) <= 2) break;
  await cua.sleep(200);
}
if (!window || Math.abs(window.bounds.width - 1440) > 2 || Math.abs(window.bounds.height - 900) > 2) {
  throw new Error('Window did not open at 1440x900: ' + JSON.stringify(window?.bounds));
}
await app.waitFor({role: 'button', name: 'Search', timeout_ms: 10000});
let rows = [];
for (let i = 0; i < 40 && rows.length !== 1; i++) {
  await cua.sleep(250);
  rows = (await app.query({role: 'button'})).filter((row) => row.label?.includes('Approval, Verify fixture'));
}
if (rows.length !== 1) throw new Error('Could not identify the Approval sidebar row');
await app.click(rows[0].element_token);
await app.waitFor({role: 'button', name: 'Approval Verify fixture', timeout_ms: 10000});
await app.click({role: 'button', name: 'Approval Verify fixture'});
await app.waitFor({role: 'button', name: 'Close side panel', timeout_ms: 5000});
const [close] = await app.query({role: 'button', name: 'Close side panel'});
if (!close || close.frame.x < 1100) throw new Error('Profile is not docked at the right edge: ' + JSON.stringify(close?.frame));
if (!(await app.query({role: 'button'})).filter((row) => row.label === 'Search').length) throw new Error('Sidebar disappeared at wide size');
const [message] = await app.query({role: 'textfield', name: 'Message'});
if (!message) throw new Error('Composer disappeared with the docked profile');
const [closeNow] = await app.query({role: 'button', name: 'Close side panel'});
await app.click(closeNow.element_token);
await app.waitFor({role: 'button', name: 'Close side panel', gone: true});
const splitters = (await app.query({role: 'splitter'})).filter((row) => row.label === 'Sidebar width');
if (!splitters.length || Math.abs((splitters[0].frame.x - window.bounds.x) - 320) > 4) throw new Error('Sidebar width changed at wide size: ' + JSON.stringify({ window: window.bounds, splitters: splitters.map((row) => ({ label: row.label, frame: row.frame })) }));
return 'ok';
