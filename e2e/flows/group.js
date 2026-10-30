// A group requires two bots, keeps selections while searching, and survives a relaunch.
let app = await cua.getApp(APP);
const stamp = Date.now();
const names = ["GroupbotA" + stamp, "GroupbotB" + stamp];
const title = "Group" + stamp;
for (const name of names) {
  await app.click({ role: "button", name: "New chat" });
  app = await pick(app, "New Bot");
  await app.typeText(name, { role: "textfield", name: "Name" });
  await app.click({ role: "button", name: "Create" });
  await app.waitFor({ role: "button", name, timeout_ms: 10000 });
}
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Group Chat");
await app.waitFor({ role: "button", name: "Close New Group Chat" });
// A disabled button may be omitted or reported display-only by the native driver.
const canCreate = async () => {
  const [button] = await app.query({ role: "button", name: "Create" });
  return !!button && !button.display_only && button.enabled !== false;
};
// Typing into a focused field adds to what's there; clearing it first lets the typing replace it.
const search = async (text) => {
  await app.setValue({ role: "textfield", name: "To" }, "");
  if (text) await app.typeText(text, { role: "textfield", name: "To" });
};
if (await canCreate() !== false) throw new Error("An empty group can be created");
await search(names[0]);
await app.click({ role: "button", name: "Select " + names[0] });
await app.waitFor({ role: "button", name: "Remove " + names[0] });
if (await canCreate() !== false) throw new Error("A one-bot group can be created");
await search(names[1]);
await app.click({ role: "button", name: "Select " + names[1] });
await app.waitFor({ role: "button", name: "Remove " + names[1] });
if (await canCreate() !== true) throw new Error("Searching lost a selected bot");
await app.typeText(title, { role: "textfield", name: "Chat name (optional)" });
await app.click({ role: "button", name: "Remove " + names[0] });
if (await canCreate() !== false) throw new Error("Removing a recipient did not update creation");
await search(names[0]);
await app.click({ role: "button", name: "Select " + names[0] });
await app.waitFor({ role: "textfield", name: "Chat name (optional)", value: title });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ text: "No messages yet.", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Close New Group Chat", gone: true, timeout_ms: 10000 });
await app.waitFor({ role: "button", name: title, timeout_ms: 10000 });
const row = (await app.query({ role: "button", name: title })).find((item) => item.label.startsWith(title + ", "));
if (!row || !names.every((name) => row.label.includes(name))) throw new Error("The group has the wrong members");
return { title, names };

// @relaunch
const app = await cua.getApp(APP);

await app.waitFor({ role: "button", name: PREVIOUS.title, timeout_ms: 10000 });
const row = (await app.query({ role: "button", name: PREVIOUS.title })).find((item) => item.label.startsWith(PREVIOUS.title + ", "));
if (!row || !PREVIOUS.names.every((name) => row.label.includes(name))) throw new Error("The group did not persist");
await app.click(row.element_token);
await app.waitFor({ role: "textfield", name: "Message" });
await app.click({ role: "button", name: "Edit chat" });
await app.waitFor({ role: "textfield", name: "Chat name" });
if ((await app.query({ name: "Default responder" })).length) throw new Error("Group replies still require a default responder setting");
// The group's editor is the chat's side panel: Add agents opens as a page inside it, with Back.
await app.click({ role: "button", name: "Add agents to chat" });
await app.waitFor({ role: "textfield", name: "Search agents to add" });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "textfield", name: "Chat name" });
await app.click({ role: "button", name: "Edit chat" });
await app.waitFor({ role: "textfield", name: "Chat name", gone: true });
return "ok";
