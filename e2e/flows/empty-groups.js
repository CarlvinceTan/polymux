// A fresh account can create bots from either empty state and return to its group draft.
let app = await cua.getApp(APP);
const stamp = Date.now();
const names = ["First" + stamp, "Second" + stamp];
await app.waitFor({ role: "button", name: "New Bot", timeout_ms: 10000 });
await app.click({ role: "button", name: "New Bot" });
await app.waitFor({ role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Close New Bot" });
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Group Chat");
await app.waitFor({ role: "statictext", name: "No bots yet." });
for (const name of names) {
  await app.click({ role: "button", name: "New Bot" });
  await app.waitFor({ role: "textfield", name: "Name" });
  await app.typeText(name, { role: "textfield", name: "Name" });
  await app.click({ role: "button", name: "Create" });
  await app.waitFor({ role: "button", name: "Select " + name });
  if (name === names[1]) await app.waitFor({ role: "button", name: "Remove " + names[0] });
  await app.click({ role: "button", name: "Select " + name });
}
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "statictext", name: "No messages yet.", timeout_ms: 10000 });
return "ok";
