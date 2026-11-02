// Characters can be selected in the native picker and used to create a bot.
const app = await cua.getApp(APP);
await app.waitFor({role: 'button', name: 'New chat', timeout_ms: 10000});
await app.click({role: 'button', name: 'New chat'});
await app.click({role: 'menuitem', name: 'New Bot'});
await app.waitFor({role: 'textfield', name: 'Name'});
await app.click((await app.query({name: 'Animals'})).find((row) => row.role === 'AXCheckBox').element_token);
await app.waitFor({name: 'Dog'});
await app.click((await app.query({name: 'Dog'})).find((row) => row.role === 'AXCheckBox').element_token);
await app.waitFor({name: 'Panda'});
await app.click((await app.query({name: 'Panda'})).find((row) => row.role === 'AXCheckBox').element_token);
await app.waitFor({name: 'Rabbit'});
await app.click((await app.query({name: 'Rabbit'})).find((row) => row.role === 'AXCheckBox').element_token);
await app.waitFor({name: 'Fox'});
await app.click((await app.query({name: 'Fox'})).find((row) => row.role === 'AXCheckBox').element_token);
await app.waitFor({name: 'Pig'});
await app.click((await app.query({name: 'Pig'})).find((row) => row.role === 'AXCheckBox').element_token);
for (const name of ['Deer', 'Cow', 'Tiger']) {
  await app.waitFor({name});
  await app.click((await app.query({name})).find((row) => row.role === 'AXCheckBox').element_token);
}
await app.waitFor({name: 'Cat'});
await app.click((await app.query({name: 'Cat'})).find((row) => row.role === 'AXCheckBox').element_token);
const name = 'Cat' + Date.now();
await app.typeText(name, {role: 'textfield', name: 'Name'});
await app.click({role: 'button', name: 'Create'});
await app.waitFor({role: 'button', name, timeout_ms: 10000});
await app.waitFor({role: 'textfield', name: 'Message', timeout_ms: 10000});
return 'ok';
