// Renaming sections preserves their chats, collapsed state, selection, and unsent draft.
let app = await cua.getApp(APP);
const state = await app.getState({ include_screenshot: false });
app = await cua.getApp({ pid: state.pid, windowId: state.window_id });
const stamp = Date.now();
const bot = "SectionBot" + stamp;
let section = "Section" + stamp;
const draft = "Keep this draft while renaming sections";
const header = () => ({ role: "button", name: "Collapse " + section });
const row = async () => {
  const chat = (await app.query({ role: "button", name: bot })).find(item => item.label?.startsWith(bot + ", "));
  if (!chat) throw new Error("Renaming removed the section's chat");
  return chat;
};
const menu = async (target, name) => {
  await app.rightClick(target);
  await app.click({ role: "menuitem", name });
};
const rename = async (target, name, cancel = false) => {
  await menu(target, "Rename");
  await app.waitFor({ role: "textfield", name: "Section name" });
  await app.setValue({ role: "textfield", name: "Section name" }, name);
  await app.pressKey(cancel ? "escape" : "return", { role: "textfield", name: "Section name" });
  await app.waitFor({ role: "textfield", name: "Section name", gone: true });
};
await app.click({ role: "button", name: "New chat" });
app = await pick(app, "New Bot");
await app.waitFor({ role: "textfield", name: "Name" });
await app.typeText(bot, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor({ role: "button", name: bot });
await app.typeText(draft, { role: "textfield", name: "Message" });
for (const mode of ["escape", "empty", "blur"]) {
  await menu((await row()).element_token, "Move to");
  await app.click({ role: "menuitem", name: "Create section" });
  await app.waitFor({ role: "textfield", name: "Section name" });
  if (mode === "empty") await app.setValue({ role: "textfield", name: "Section name" }, "");
  if (mode === "blur") {
    await cua.sleep(700);
    await app.pressKey("tab", { role: "textfield", name: "Section name" });
  } else {
    await app.pressKey(mode === "empty" ? "return" : "escape", { role: "textfield", name: "Section name" });
  }
  await app.waitFor({ role: "textfield", name: "Section name", gone: true });
  if ((await app.query({ role: "button", name: "Collapse New section" })).length) {
    throw new Error("Cancelling created a section");
  }
  await row();
}
await menu((await row()).element_token, "Move to");
await app.click({ role: "menuitem", name: "Create section" });
await app.waitFor({ role: "textfield", name: "Section name" });
await app.setValue({ role: "textfield", name: "Section name" }, "x".repeat(41));
await app.pressKey("return", { role: "textfield", name: "Section name" });
await app.waitFor({ role: "statictext", name: "Section names are limited to 40 characters." });
await app.setValue({ role: "textfield", name: "Section name" }, section);
// The local draft must remain editable until the name is confirmed.
await cua.sleep(1000);
await app.waitFor({ role: "textfield", name: "Section name", value: section });
await app.pressKey("return", { role: "textfield", name: "Section name" });
await app.waitFor(header());
const before = await row();
for (const name of [section + " renamed", section + " again"]) {
  await rename(header(), "  " + name + "  ");
  section = name;
  await app.waitFor(header());
  const after = await row();
  if (!after.frame || !before.frame || after.frame.y !== before.frame.y || after.frame.h !== before.frame.h) {
    throw new Error("Renaming moved or resized the section's chat");
  }
  await app.waitFor({ role: "textfield", name: "Message", value: draft });
}
await rename(header(), "Discarded name", true);
await app.waitFor(header());
await app.click(header());
await app.waitFor({ role: "button", name: "Expand " + section });
await rename({ role: "button", name: "Expand " + section }, section + " collapsed");
section += " collapsed";
await app.waitFor({ role: "button", name: "Expand " + section });
if ((await app.query({ role: "button", name: bot })).some(item => item.label?.startsWith(bot + ", "))) {
  throw new Error("Renaming expanded a collapsed section");
}
await app.click({ role: "button", name: "Expand " + section });
await app.waitFor(header());
await row();
await app.waitFor({ role: "textfield", name: "Message", value: draft });
return { section, bot, draft };

// @relaunch
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Collapse " + PREVIOUS.section, timeout_ms: 10000 });
await app.waitFor({ role: "button", name: PREVIOUS.bot });
await app.waitFor({ role: "textfield", name: "Message", value: PREVIOUS.draft });
return "ok";
