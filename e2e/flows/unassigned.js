// Unassigned excludes pins and assigned chats, and persists its section controls.
let app = await cua.getApp(APP);
const state = await app.getState({ include_screenshot: false });
app = await cua.getApp({ pid: state.pid, windowId: state.window_id });
const stamp = Date.now();
const name = "Unassigned" + stamp;
const section = "Work" + stamp;
const header = { role: "button", name: "Collapse Unassigned" };
const row = async () => {
  const item = (await app.query({ role: "button", name })).find(item => item.label.startsWith(name + ", "));
  if (!item) throw new Error("The test chat is missing");
  return item.element_token;
};
const menu = async (target, item) => {
  await app.rightClick(target);
  await app.click({ role: "menuitem", name: item });
};
// Earlier flows may have left chats in this shared throwaway instance.
if ((await app.query({ role: "button", name: "Expand Unassigned" })).length) {
  await app.click({ role: "button", name: "Expand Unassigned" });
}
if ((await app.query(header)).length) {
  const earlier = "Earlier chats " + Date.now();
  await menu(header, "Create section");
  await app.waitFor({ role: "textfield", name: "Section name" });
  await app.setValue({ role: "textfield", name: "Section name" }, earlier);
  await app.pressKey("return", { role: "textfield", name: "Section name" });
  await app.waitFor({ role: "button", name: "Collapse " + earlier });
  for (let i = 0; i < 100 && (await app.query(header)).length; i++) {
    const buttons = await app.query({ role: "button" });
    const index = buttons.findIndex(item => item.label === header.name);
    const chat = buttons[index + 1];
    if (!chat || /^(Collapse|Expand) /.test(chat.label)) {
      await cua.sleep(100);
      continue;
    }
    await app.rightClick({ role: "button", name: chat.label });
    await app.click({ role: "menuitem", name: "Move to" });
    await app.click({ role: "menuitem", name: earlier });
  }
  await app.waitFor({ ...header, gone: true });
}
await app.click({ role: "button", name: "New chat" });
await app.click({ role: "menuitem", name: "New Bot" });
await app.waitFor({ role: "textfield", name: "Name" });
await app.typeText(name, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
await app.waitFor(header);
await menu(await row(), "Pin");
await app.waitFor({ ...header, gone: true });
await app.waitFor({ role: "button", name: name + ", pinned chat" });
await menu({ role: "button", name: name + ", pinned chat" }, "Unpin");
await app.waitFor(header);
await app.rightClick(header);
if ((await app.query({ role: "menuitem", name: "Rename" })).length ||
    (await app.query({ role: "menuitem", name: "Delete section" })).length) {
  throw new Error("Unassigned can be renamed or deleted");
}
await app.click({ role: "menuitem", name: "Create section" });
await app.waitFor({ role: "textfield", name: "Section name" });
await app.setValue({ role: "textfield", name: "Section name" }, section);
await app.pressKey("return", { role: "textfield", name: "Section name" });
await app.waitFor({ role: "button", name: "Collapse " + section });
// Put Work directly after Unassigned even when earlier flows created sections.
let headers = await app.query({ role: "button" });
while (headers.findIndex(item => item.label === "Collapse " + section) > headers.findIndex(item => item.label === header.name) + 2) {
  await menu({ role: "button", name: "Collapse " + section }, "Move up");
  await cua.sleep(200);
  headers = await app.query({ role: "button" });
}
await app.rightClick(await row());
await app.click({ role: "menuitem", name: "Move to" });
await app.click({ role: "menuitem", name: section });
await app.waitFor({ ...header, gone: true });
await app.rightClick(await row());
await app.click({ role: "menuitem", name: "Move to" });
await app.click({ role: "menuitem", name: "Unassigned" });
await app.waitFor(header);
await app.click(header);
await app.waitFor({ role: "button", name: "Expand Unassigned" });
if ((await app.query({ role: "button", name })).some(item => item.label.startsWith(name + ", "))) {
  throw new Error("Collapsing Unassigned left its chat visible");
}
await menu({ role: "button", name: "Expand Unassigned" }, "Move down");
const moved = async () => {
  const ordered = await app.query({ role: "button" });
  const work = ordered.findIndex(item => item.label === "Collapse " + section);
  const free = ordered.findIndex(item => item.label === "Expand Unassigned");
  return work >= 0 && free > work;
};
for (let i = 0; i < 30 && !(await moved()); i++) await cua.sleep(100);
if (!(await moved())) {
  throw new Error("Unassigned did not move below Work: " + JSON.stringify(await app.query({ role: "button" })));
}
return { name, section };

// @relaunch
const initial = await cua.getApp(APP);
const state = await initial.getState({ include_screenshot: false });
const app = await cua.getApp({ pid: state.pid, windowId: state.window_id });
await app.waitFor({ role: "button", name: "Expand Unassigned", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Collapse " + PREVIOUS.section });
const ordered = await app.query({ role: "button" });
if (ordered.findIndex(item => item.label === "Collapse " + PREVIOUS.section) >= ordered.findIndex(item => item.label === "Expand Unassigned")) {
  throw new Error("Unassigned order did not survive relaunch");
}
await app.click({ role: "button", name: "Expand Unassigned" });
await app.waitFor({ role: "button", name: "Collapse Unassigned" });
if (!(await app.query({ role: "button", name: PREVIOUS.name })).some(item => item.label.startsWith(PREVIOUS.name + ", "))) {
  throw new Error("Expanding Unassigned did not restore its chat");
}
return "ok";
