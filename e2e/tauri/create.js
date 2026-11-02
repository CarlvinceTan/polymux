// A new bot opens its creation form and its saved chat.
const app = await cua.getApp(APP);
await app.waitFor({role: 'button', name: 'New chat', timeout_ms: 10000});
await app.click({role: 'button', name: 'New chat'});
await app.click({role: 'menuitem', name: 'New Bot'});
await app.waitFor({role: 'textfield', name: 'Name'});
const name = 'Bot' + Date.now();
await app.typeText(name, {role: 'textfield', name: 'Name'});
await app.click({role: 'button', name: 'Create'});
await app.waitFor({role: 'button', name, timeout_ms: 10000});
await app.waitFor({role: 'textfield', name: 'Message', timeout_ms: 10000});
await app.typeText('Draft text', {role: 'textfield', name: 'Message'});
await app.waitFor({role: 'textfield', name: 'Message', value: 'Draft text'});
return 'ok';
