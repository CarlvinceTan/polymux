// Replies survive relaunch and can navigate back to the original message.
const app = await cua.getApp(APP);
await app.waitFor({role: 'button', name: 'New chat', timeout_ms: 10000});
await app.click({role: 'button', name: 'New chat'});
await app.click({role: 'menuitem', name: 'New Bot'});
await app.typeText('Reply' + Date.now(), {role: 'textfield', name: 'Name'});
await app.click({role: 'button', name: 'Create'});
await app.waitFor({role: 'textfield', name: 'Message', timeout_ms: 10000});
await app.typeText('Original message', {role: 'textfield', name: 'Message'});
await app.pressKey('return', {role: 'textfield', name: 'Message'});
await app.waitFor({role: 'button', name: 'Original message', timeout_ms: 10000});
await app.rightClick({role: 'button', name: 'Original message'});
await app.waitFor({name: 'Reply'});
const replyAction = (await app.query({name: 'Reply'})).find((row) => row.role === 'AXMenuItem');
if (!replyAction) throw new Error('Missing Reply menu item');
await app.click(replyAction.element_token);
await app.waitFor({role: 'button', name: 'Cancel reply'});
await cua.sleep(1200);
// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({role: 'button', name: 'Cancel reply', timeout_ms: 10000});
await app.typeText('Reply message', {role: 'textfield', name: 'Message'});
await app.pressKey('return', {role: 'textfield', name: 'Message'});
await app.waitFor({role: 'button', name: 'Reply to You: Original message', timeout_ms: 10000});
await app.click({role: 'button', name: 'Reply to You: Original message'});
return 'ok';
