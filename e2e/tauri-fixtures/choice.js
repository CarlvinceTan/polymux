// Run with scripts/run-e2e.sh --tauri --verify-server e2e/tauri-fixtures/choice.js
const app = await cua.getApp(APP);
await app.waitFor({role: 'button', name: 'Approval', timeout_ms: 10000});
if (!(await app.query({role: 'button', name: 'A. Plan my day'})).length) {
  const rows = (await app.query({role: 'button', name: 'Approval'})).filter((row) =>
    row.label?.includes('Approval, Verify fixture'));
  if (rows.length !== 1) throw new Error('Could not identify the Approval sidebar row');
  await app.click(rows[0].element_token);
}
await app.waitFor({role: 'button', name: 'A. Plan my day', timeout_ms: 10000});
await app.waitFor({role: 'button', name: 'B. Build something'});
await app.click({role: 'button', name: 'A. Plan my day'});
await app.waitFor({role: 'button', name: 'A. Plan my day', gone: true});
await app.waitFor({role: 'button', name: 'Dismiss question', gone: true});
return 'ok';
// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({role: 'button', name: 'Approval', timeout_ms: 10000});
const rows = (await app.query({role: 'button', name: 'Approval'})).filter((row) =>
  row.label?.includes('Approval, Verify fixture'));
if (rows.length !== 1) throw new Error('Could not identify the Approval sidebar row after relaunch');
await app.click(rows[0].element_token);
await cua.sleep(500);
await app.waitFor({role: 'button', name: 'Reply to Approval: What should we focus on today?', timeout_ms: 10000});
const state = await app.getState({include_screenshot: false});
if (!state.tree_markdown?.includes('AXStaticText = "What should we focus on today?"') ||
    !state.tree_markdown.includes('AXStaticText = "Plan my day"')) throw new Error('Saved choice is not visible');
await app.waitFor({role: 'button', name: 'A. Plan my day', gone: true});
await app.waitFor({role: 'button', name: 'Dismiss question', gone: true});
return 'ok';
