// Unassigned shows only beside other sections, excludes pins and assigned chats, and persists its section controls.
let app = await cua.getApp(APP);
const state = await app.getState({ include_screenshot: false });
// pick() can return a handle on a menu's window, so menus hand back the main window.
const main = () => cua.getApp({ pid: state.pid, windowId: state.window_id });
app = await main();
const stamp = Date.now();
const name = "Unassigned" + stamp;
const section = "Work" + stamp;
const header = { role: "button", name: "Collapse Unassigned" };
const headers = ["Collapse Unassigned", "Expand Unassigned"];
const find = async (handle = app) => (await handle.query({ role: "button", name }))
  .find(item => item.label.startsWith(name + ", "));
// A button by its exact label, or a chat row by its title.
const element = async (handle, label) => {
  const item = (await handle.query({ role: "button", name: label }))
    .find(item => item.label === label || item.label.startsWith(label + ", "));
  if (!item) throw new Error("Missing " + label);
  return item.element_token;
};
// Opens a row's menu and chooses an item, reopening it if input elsewhere closed it.
const menu = async (label, item) => {
  await app.rightClick(await element(app, label));
  await pick(app, item, async next => next.rightClick(await element(next, label)));
  app = await main();
};
const removeSection = async label => {
  await menu(label, "Delete section");
  await app.waitFor({ role: "button", name: "Cancel" });
  const [keep] = (await app.query({ role: "button", name: "Delete section" })).filter(item => item.label === "Delete section");
  if (!keep) throw new Error("The section delete dialog is missing");
  await app.click(keep.element_token);
  await app.waitFor({ role: "button", name: label, gone: true });
};
// Earlier flows may have left sections and chats in this shared throwaway instance.
for (let i = 0; i < 40; i++) {
  const custom = (await app.query({ role: "button" }))
    .find(item => /^(Collapse|Expand) /.test(item.label) && !headers.includes(item.label));
  if (!custom) break;
  await removeSection(custom.label);
}
await app.click({ role: "button", name: "New chat" });
await pick(app, "New Bot");
app = await main();
await app.waitFor({ role: "textfield", name: "Name" });
await app.typeText(name, { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Create" });
for (let i = 0; i < 40 && !(await find()); i++) await cua.sleep(100);
if (!(await find())) throw new Error("The test chat is missing");
if ((await app.query({ role: "button" })).some(item => headers.includes(item.label))) {
  throw new Error("Unassigned has a header without other sections");
}
await menu(name, "Move to");
await app.waitFor({ role: "menuitem", name: "Create section" });
if ((await app.query({ role: "menuitem", name: "Unassigned" })).some(item => item.label === "Unassigned")) {
  throw new Error("Move to offers Unassigned without other sections");
}
await app.click({ role: "menuitem", name: "Create section" });
await app.waitFor({ role: "textfield", name: "Section name" });
await app.setValue({ role: "textfield", name: "Section name" }, section);
await app.pressKey("return", { role: "textfield", name: "Section name" });
await app.waitFor({ role: "button", name: "Collapse " + section });
// Move the earlier chats into the new section, so Unassigned holds only the test chat.
if ((await app.query({ role: "button", name: "Expand Unassigned" })).length) {
  await app.click({ role: "button", name: "Expand Unassigned" });
}
for (let i = 0; i < 100 && (await app.query(header)).length; i++) {
  const buttons = await app.query({ role: "button" });
  const index = buttons.findIndex(item => item.label === header.name);
  const chat = buttons[index + 1];
  if (!chat || /^(Collapse|Expand) /.test(chat.label)) {
    await cua.sleep(100);
    continue;
  }
  await menu(chat.label, "Move to");
  await app.click({ role: "menuitem", name: section });
}
await app.waitFor({ ...header, gone: true });
await menu(name, "Move to");
await app.click({ role: "menuitem", name: "Unassigned" });
await app.waitFor(header);
await menu(name, "Pin");
await app.waitFor({ ...header, gone: true });
await app.waitFor({ role: "button", name: name + ", pinned chat" });
await menu(name + ", pinned chat", "Unpin");
await app.waitFor(header);
await app.rightClick(header);
await app.waitFor({ role: "menuitem", name: "Move down" });
if ((await app.query({ role: "menuitem", name: "Rename" })).length ||
    (await app.query({ role: "menuitem", name: "Delete section" })).length) {
  throw new Error("Unassigned can be renamed or deleted");
}
await pick(app, "Move down", next => next.rightClick(header));
app = await main();
const moved = async () => {
  const ordered = await app.query({ role: "button" });
  const work = ordered.findIndex(item => item.label === "Collapse " + section);
  const free = ordered.findIndex(item => headers.includes(item.label));
  return work >= 0 && free > work;
};
for (let i = 0; i < 30 && !(await moved()); i++) await cua.sleep(100);
if (!(await moved())) {
  throw new Error("Unassigned did not move below Work: " + JSON.stringify(await app.query({ role: "button" })));
}
await app.click(header);
await app.waitFor({ role: "button", name: "Expand Unassigned" });
if (await find()) throw new Error("Collapsing Unassigned left its chat visible");
return { name, section };

// @relaunch
const initial = await cua.getApp(APP);
const state = await initial.getState({ include_screenshot: false });
const main = () => cua.getApp({ pid: state.pid, windowId: state.window_id });
let app = await main();
const shown = async () => (await app.query({ role: "button", name: PREVIOUS.name }))
  .some(item => item.label.startsWith(PREVIOUS.name + ", "));
await app.waitFor({ role: "button", name: "Expand Unassigned", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Collapse " + PREVIOUS.section });
const ordered = await app.query({ role: "button" });
if (ordered.findIndex(item => item.label === "Collapse " + PREVIOUS.section) >= ordered.findIndex(item => item.label === "Expand Unassigned")) {
  throw new Error("Unassigned order did not survive relaunch");
}
// Without other sections, the collapsed Unassigned loses its header and shows its chats.
const work = { role: "button", name: "Collapse " + PREVIOUS.section };
await app.rightClick(work);
await pick(app, "Delete section", next => next.rightClick(work));
app = await main();
await app.waitFor({ role: "button", name: "Cancel" });
const [keep] = (await app.query({ role: "button", name: "Delete section" })).filter(item => item.label === "Delete section");
if (!keep) throw new Error("The section delete dialog is missing");
await app.click(keep.element_token);
await app.waitFor({ role: "button", name: "Expand Unassigned", gone: true });
for (let i = 0; i < 30 && !(await shown()); i++) await cua.sleep(100);
if (!(await shown())) throw new Error("Unassigned chats are hidden without other sections");
if ((await app.query({ role: "button" })).some(item => item.label === "Collapse Unassigned")) {
  throw new Error("Unassigned has a header without other sections");
}
return "ok";
