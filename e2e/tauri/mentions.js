// A group mention is selected from the picker and its message survives relaunch.
const app = await cua.getApp(APP);
const stamp = Date.now();
const names = [`MentionAlpha${stamp}`, `MentionBeta${stamp}`];
const group = `Mentions${stamp}`;
for (const name of names) {
  await app.click({role: 'button', name: 'New chat'});
  await app.click({role: 'menuitem', name: 'New Bot'});
  await app.typeText(name, {role: 'textfield', name: 'Name'});
  await app.click({role: 'button', name: 'Create'});
  await app.waitFor({role: 'button', name, timeout_ms: 10000});
}
await app.click({role: 'button', name: 'New chat'});
await app.click({role: 'menuitem', name: 'New Group Chat'});
for (const name of names) await app.click({role: 'button', name: `Select ${name}`});
await app.typeText(group, {role: 'textfield', name: 'Chat name (optional)'});
await app.click({role: 'button', name: 'Create'});
await app.waitFor({role: 'textfield', name: 'Message', timeout_ms: 10000});
await app.typeText('@MentionAlpha', {role: 'textfield', name: 'Message'});
await app.waitFor({role: 'button', name: `Mention ${names[0]}`, timeout_ms: 10000});
await app.pressKey('return', {role: 'textfield', name: 'Message'});
await app.waitFor({role: 'textfield', name: 'Message', value: `@${names[0]} `});
await app.typeText('please review', {role: 'textfield', name: 'Message'});
await app.pressKey('return', {role: 'textfield', name: 'Message'});
const message = `@${names[0]} please review`;
await app.waitFor({role: 'button', name: message, timeout_ms: 10000});
await cua.sleep(1200);
return {group, message};

// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({role: 'button', name: PREVIOUS.group, timeout_ms: 10000});
await app.click({role: 'button', name: PREVIOUS.group});
await app.waitFor({role: 'button', name: PREVIOUS.message, timeout_ms: 10000});
return 'ok';
