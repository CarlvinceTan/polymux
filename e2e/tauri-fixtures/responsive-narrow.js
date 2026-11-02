// @size 780x772
// Run with scripts/run-e2e.sh --verify-server e2e/tauri-fixtures/responsive-narrow.js
const app = await cua.getApp(APP);
let window;
for (let i = 0; i < 50; i++) {
  window = (await cua.listWindows({app: APP}))[0];
  if (window && Math.abs(window.bounds.width - 780) <= 2 && Math.abs(window.bounds.height - 772) <= 2) break;
  await cua.sleep(200);
}
if (!window || Math.abs(window.bounds.width - 780) > 2 || Math.abs(window.bounds.height - 772) > 2) {
  throw new Error('Window did not open at 780x772: ' + JSON.stringify(window?.bounds));
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
if (!close || close.frame.x < 500) throw new Error('Profile is not full width: ' + JSON.stringify(close?.frame));
await cua.sleep(300);
if ((await app.query({role: 'button'})).filter((row) => row.label === 'Search').length) throw new Error('Sidebar remained visible behind the full-page profile');
const [closeNow] = await app.query({role: 'button', name: 'Close side panel'});
await app.click(closeNow.element_token);
await app.waitFor({role: 'button', name: 'Search', timeout_ms: 5000});
const splitters = (await app.query({role: 'splitter'})).filter((row) => row.label === 'Sidebar width');
if (!splitters.length || Math.abs((splitters[0].frame.x - window.bounds.x) - 320) > 4) throw new Error('Sidebar width changed at narrow size: ' + JSON.stringify({ window: window.bounds, splitters: splitters.map((row) => ({ label: row.label, frame: row.frame })) }));
return 'ok';
