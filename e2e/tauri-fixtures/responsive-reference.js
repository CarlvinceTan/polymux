// @size 1060x772
// Run with scripts/run-e2e.sh --verify-server e2e/tauri-fixtures/responsive-reference.js
const app = await cua.getApp(APP);
let window;
for (let i = 0; i < 50; i++) {
  window = (await cua.listWindows({app: APP}))[0];
  if (window && Math.abs(window.bounds.width - 1060) <= 2 && Math.abs(window.bounds.height - 772) <= 2) break;
  await cua.sleep(200);
}
if (!window || Math.abs(window.bounds.width - 1060) > 2 || Math.abs(window.bounds.height - 772) > 2) {
  throw new Error('Window did not open at 1060x772: ' + JSON.stringify(window?.bounds));
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
const splitters = (await app.query({role: 'splitter'})).filter((row) => row.label === 'Sidebar width');
const sidebar = splitters.length ? splitters[0].frame.x - window.bounds.x : NaN;
if (Math.abs(sidebar - 320) > 4) throw new Error('Sidebar width is not 320: ' + sidebar);
const [add] = await app.query({role: 'button', name: 'Add attachment'});
const [message] = await app.query({role: 'textfield', name: 'Message'});
if (!add || Math.abs(add.frame.h - 32) > 1) throw new Error('Attachment control is not 32 px: ' + JSON.stringify(add?.frame));
if (!message || message.frame.w < 300) throw new Error('Composer field is missing or too narrow: ' + JSON.stringify(message?.frame));
await app.click({role: 'button', name: 'Approval Verify fixture'});
await app.waitFor({role: 'button', name: 'Close side panel', timeout_ms: 5000});
const [close] = await app.query({role: 'button', name: 'Close side panel'});
if (!close || close.frame.x - window.bounds.x < 700) throw new Error('Profile is not docked at 320 px: ' + JSON.stringify(close?.frame));
const [closeNow] = await app.query({role: 'button', name: 'Close side panel'});
await app.click(closeNow.element_token);
return 'ok';
