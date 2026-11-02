// A sent message can be reacted to and the reaction removed in the native app.
const app = await cua.getApp(APP);
await app.waitFor({role: 'button', name: 'New chat', timeout_ms: 10000});
const openPanel = await app.query({role: 'button', name: 'Close side panel'});
if (openPanel.length) await app.click(openPanel[0].element_token);
await app.click({role: 'button', name: 'New chat'});
await app.click({role: 'menuitem', name: 'New Bot'});
await app.typeText('Reaction' + Date.now(), {role: 'textfield', name: 'Name'});
await app.click({role: 'button', name: 'Create'});
await app.waitFor({role: 'textfield', name: 'Message', timeout_ms: 10000});
await app.click({role: 'textfield', name: 'Message'});
await app.typeText('Reaction test message', {role: 'textfield', name: 'Message'});
await app.click({role: 'button', name: 'Send message'});
await app.waitFor({role: 'button', name: 'Reaction test message', timeout_ms: 10000});
await app.waitFor({text: 'Sending…', gone: true, timeout_ms: 10000});
await cua.sleep(2000);
let selected = false;
for (let attempt = 0; attempt < 8 && !selected; attempt++) {
  await app.click({role: 'button', name: 'Reaction test message'});
  await app.rightClick({role: 'button', name: 'Reaction test message'});
  await cua.sleep(500);
  const choice = (await app.query({name: 'React 👍'})).find((row) => row.element_token && row.enabled !== false);
  if (choice) {
    await app.click(choice.element_token);
    selected = true;
    break;
  }
  await cua.sleep(500);
}
if (!selected) throw new Error('The message did not become available for reactions');
await app.waitFor({name: 'Reaction 👍, 1, You', timeout_ms: 10000});
let reaction;
for (let i = 0; i < 30; i++) {
  reaction = (await app.query({name: 'Reaction 👍, 1, You'})).find((row) => row.role === 'AXCheckBox' && row.enabled);
  if (reaction) break;
  await cua.sleep(100);
}
if (!reaction?.element_token) throw new Error('Reaction did not finish saving');
await app.click(reaction.element_token);
await app.waitFor({name: 'Reaction 👍, 1, You', gone: true, timeout_ms: 10000});
return 'ok';
