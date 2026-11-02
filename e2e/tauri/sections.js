// The inline section editor stays open after its menu closes in the native app.
const app = await cua.getApp(APP);
await app.waitFor({role: 'button', name: 'New chat', timeout_ms: 10000});
await app.click({role: 'button', name: 'New chat'});
await app.click({role: 'menuitem', name: 'New Bot'});
await app.typeText('SectionBot' + Date.now(), {role: 'textfield', name: 'Name'});
await app.click({role: 'button', name: 'Create'});
await app.waitFor({role: 'button', name: 'Collapse Unassigned', timeout_ms: 10000});
await app.rightClick({role: 'button', name: 'Collapse Unassigned'});
await app.click({role: 'menuitem', name: 'Create section'});
const name = 'Section' + Date.now();
await app.typeText(name, {role: 'textfield', name: 'Section name'});
await app.waitFor({role: 'textfield', name: 'Section name', value: name});
return 'ok';
