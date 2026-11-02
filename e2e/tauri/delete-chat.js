// Confirming a group deletion leaves its bots; cancelling a bot deletion keeps it.
const app = await cua.getApp(APP);
const suffix = Date.now();
const alpha = `DeleteAlpha${suffix}`;
const beta = `DeleteBeta${suffix}`;
const group = `DeleteGroup${suffix}`;
// A group row lists its agents, so match agent rows by their title position, not a substring.
async function agentRow(name) {
  for (let i = 0; i < 40; i++) {
    const rows = (await app.query({role: 'button'})).filter((row) => {
      const parts = (row.label ?? '').split(', ');
      return parts.indexOf(name) > 0 && parts.slice(0, parts.indexOf(name)).every((part) => part === 'Ready' || part === 'Unread');
    });
    if (rows.length === 1) return rows[0];
    await cua.sleep(250);
  }
  throw new Error(`Could not identify the ${name} sidebar row`);
}
await app.waitFor({role: 'button', name: 'New chat', timeout_ms: 10000});
for (const name of [alpha, beta]) {
  await app.click({role: 'button', name: 'New chat'});
  await app.click({role: 'menuitem', name: 'New Bot'});
  await app.typeText(name, {role: 'textfield', name: 'Name'});
  await app.click({role: 'button', name: 'Create'});
  await agentRow(name);
}
await app.click({role: 'button', name: 'New chat'});
await app.click({role: 'menuitem', name: 'New Group Chat'});
await app.click({role: 'button', name: `Select ${alpha}`});
await app.click({role: 'button', name: `Select ${beta}`});
await app.typeText(group, {role: 'textfield', name: 'Chat name (optional)'});
await app.click({role: 'button', name: 'Create'});
await app.waitFor({role: 'button', name: group, timeout_ms: 10000});
await app.click((await agentRow(beta)).element_token);
await app.rightClick({role: 'button', name: group});
await app.click({role: 'menuitem', name: 'Delete'});
await app.waitFor({name: 'Delete chat?'});
await app.click({role: 'button', name: 'Delete'});
await app.waitFor({role: 'button', name: group, gone: true, timeout_ms: 10000});
await agentRow(alpha);
await app.rightClick((await agentRow(alpha)).element_token);
await app.click({role: 'menuitem', name: 'Delete'});
await app.waitFor({name: 'Delete agent?'});
await app.click({role: 'button', name: 'Cancel'});
await agentRow(alpha);
await app.rightClick((await agentRow(alpha)).element_token);
await app.click({role: 'menuitem', name: 'Delete'});
await app.waitFor({name: 'Delete agent?'});
await app.click({role: 'button', name: 'Delete'});
await app.waitFor({role: 'button', name: alpha, gone: true, timeout_ms: 10000});
await agentRow(beta);
await cua.sleep(4000);
return {alpha, beta, group};
// @relaunch
const app = await cua.getApp(APP);
const {alpha, beta, group} = PREVIOUS;
await app.waitFor({role: 'button', name: beta, timeout_ms: 10000});
await app.waitFor({role: 'button', name: group, gone: true});
await app.waitFor({role: 'button', name: alpha, gone: true});
return 'ok';
